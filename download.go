package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	runtimedebug "runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iyear/gowidevine"
	"github.com/unki2aut/go-mpd"
)

const maxWorkers = 10

// episodeMode selects what a download writes: the usual merged MKV, or only
// audio/subtitles as per-language sidecar files.
type episodeMode int

const (
	modeFull episodeMode = iota
	modeAudioOnly
	modeSubsOnly
)

// audioTrackExt is the extension used for the per-language audio files that
// -audio-only writes. The decrypted track is a fragmented MP4 carrying AAC.
const audioTrackExt = "m4a"

// currentMode reports the output mode selected by the command-line flags.
func currentMode() episodeMode {
	switch {
	case *audioOnly:
		return modeAudioOnly
	case *subsOnly:
		return modeSubsOnly
	default:
		return modeFull
	}
}

// seasonFolderName returns the per-season subdirectory used by -audio-only and
// -subs-only output, e.g. "Season 01".
func seasonFolderName(season int) string {
	return fmt.Sprintf("Season %02d", season)
}

// episodeBaseName is the "S01E01 - Title" stem shared by the merged MKV and by
// the per-language files written in -audio-only/-subs-only mode.
func episodeBaseName(info EpisodeInfo) string {
	return fmt.Sprintf("S%02dE%02d - %s",
		info.EpisodeMetadata.SeasonNumber,
		info.EpisodeMetadata.EpisodeNumber,
		sanitizeFilename(info.Title),
	)
}

// sidecarDir returns (and creates) the per-language output folder used by the
// audio-only and subtitles-only modes: series/Season NN/<locale>.
func sidecarDir(series, seasonFolder, locale string) (string, error) {
	dir := filepath.Join(series, seasonFolder, locale)
	if err := os.MkdirAll(dir, 0777); err != nil {
		return "", err
	}
	return dir, nil
}

// sidecarName builds the file name for one track in its per-language folder,
// tagging closed captions so they can't collide with a same-format subtitle.
func sidecarName(baseName, ext string, isCC bool) string {
	suffix := ""
	if isCC {
		suffix = " [CC]"
	}
	return baseName + suffix + "." + ext
}

func buildUrl(base, representationId, file string, partNum *int64) string {
	if partNum != nil {
		file = strings.ReplaceAll(file, "$Number$", fmt.Sprintf("%05d", *partNum))
		file = strings.ReplaceAll(file, "$Number%05d$", fmt.Sprintf("%05d", *partNum))
	}
	return base + strings.ReplaceAll(file, "$RepresentationID$", representationId)
}

func downloadPart(url string) ([]byte, error) {
	maxRetries := 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Origin", "https://static.crunchyroll.com")
		req.Header.Set("Referer", "https://static.crunchyroll.com/")
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if attempt < maxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, err)
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			if attempt < maxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("failed after %d retries, status: %d", maxRetries, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			if attempt < maxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("failed reading body after %d retries: %w", maxRetries, err)
		}
		return body, nil
	}
	return nil, fmt.Errorf("failed after %d retries", maxRetries)
}

func getFilename(set *mpd.AdaptationSet, subExt string) string {
	if set == nil {
		if subExt == "" {
			subExt = "ass"
		}
		f, _ := os.CreateTemp("", "crdl-subs-*."+subExt)
		return f.Name()
	}
	for _, representation := range set.Representations {
		if representation.Height != nil {
			f, _ := os.CreateTemp("", "crdl-video-*.mp4")
			return f.Name()
		} else if representation.Bandwidth != nil {
			f, _ := os.CreateTemp("", "crdl-audio-*.mp3")
			return f.Name()
		}
	}
	return ""
}

// maxBufferedSegments bounds how many segments may be resident in memory at
// once, counting both in-flight downloads and payloads still waiting to be
// written. Without it the workers race arbitrarily far ahead of the sequential
// writer whenever an early segment is slow, which is what exhausted memory on
// movie-length titles.
const maxBufferedSegments = maxWorkers * 2

// streamSegments fetches every url concurrently but writes the payloads to w
// strictly in index order, releasing each one as soon as it has been written.
// Peak memory is therefore bounded by maxBufferedSegments rather than growing
// with the length of the media.
//
// onProgress, if non-nil, is called with the running count of fetched segments.
func streamSegments(w io.Writer, urls []string, fetch func(string) ([]byte, error), onProgress func(fetched int64)) error {
	total := len(urls)
	if total == 0 {
		return nil
	}

	var (
		mu      sync.Mutex
		cond    = sync.NewCond(&mu)
		payload = make([][]byte, total)
		ready   = make([]bool, total)
		failure error
	)

	abort := make(chan struct{})
	var abortOnce sync.Once
	fail := func(err error) {
		mu.Lock()
		if failure == nil {
			failure = err
		}
		mu.Unlock()
		cond.Broadcast()
		abortOnce.Do(func() { close(abort) })
	}

	// A worker claims a slot before claiming an index, so the indices held at
	// any moment are always the lowest outstanding ones. That ordering is what
	// guarantees the writer's next index is always held by a live worker and
	// can never be starved by later segments hogging every slot.
	slots := make(chan struct{}, maxBufferedSegments)
	var next atomic.Int64
	var fetched atomic.Int64

	var wg sync.WaitGroup
	for n := 0; n < maxWorkers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case slots <- struct{}{}:
				case <-abort:
					return
				}

				i := int(next.Add(1) - 1)
				if i >= total {
					<-slots
					return
				}

				data, err := fetch(urls[i])
				if err != nil {
					fail(err)
					return
				}

				mu.Lock()
				payload[i] = data
				ready[i] = true
				mu.Unlock()
				cond.Broadcast()

				if onProgress != nil {
					onProgress(fetched.Add(1))
				}
			}
		}()
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := 0; i < total; i++ {
			mu.Lock()
			for !ready[i] && failure == nil {
				cond.Wait()
			}
			if failure != nil {
				mu.Unlock()
				return
			}
			data := payload[i]
			payload[i] = nil
			mu.Unlock()

			_, err := w.Write(data)
			<-slots
			if err != nil {
				fail(fmt.Errorf("writing segment %d: %w", i, err))
				return
			}
		}
	}()

	wg.Wait()
	<-writerDone

	mu.Lock()
	defer mu.Unlock()
	return failure
}

func downloadParts(title string, baseUrl, representationId *string, set *mpd.AdaptationSet, keys []*widevine.Key) (string, error) {
	initUrl := buildUrl(*baseUrl, *representationId, *set.SegmentTemplate.Initialization, nil)
	initData, err := downloadPart(initUrl)
	if err != nil {
		return "", err
	}

	timeline := expandTimeline(set.SegmentTemplate.SegmentTimeline.S, 1)
	total := len(timeline)
	urls := make([]string, total)
	for i, item := range timeline {
		urls[i] = buildUrl(*baseUrl, *representationId, *set.SegmentTemplate.Media, &item)
	}

	filename := getFilename(set, "")
	encPath := filename + ".enc"
	encFile, err := os.Create(encPath)
	if err != nil {
		return "", err
	}
	defer os.Remove(encPath)
	defer encFile.Close()

	if _, err = encFile.Write(initData); err != nil {
		return "", fmt.Errorf("writing init segment: %w", err)
	}

	bar := newProgressBar(title, int64(total), "segments")
	var totalBytes atomic.Int64
	fetch := func(url string) ([]byte, error) {
		data, err := downloadPart(url)
		if err == nil {
			totalBytes.Add(int64(len(data)))
		}
		return data, err
	}
	err = streamSegments(encFile, urls, fetch, func(fetched int64) {
		bar.update(fetched, totalBytes.Load())
	})
	bar.finish()
	if err != nil {
		return "", err
	}

	if _, err = encFile.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewinding %s: %w", encPath, err)
	}

	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if err = decryptMP4(initData, encFile, keys, file); err != nil {
		return "", fmt.Errorf("decryptMP4: %w", err)
	}

	return filename, nil
}

// downloadAudioTrack downloads a version's audio representation into a temporary
// file. sets is non-nil only for on-demand manifests. When caching is enabled a
// previously downloaded copy is returned instead of re-fetching the segments.
func downloadAudioTrack(manifest *mpd.MPD, sets []onDemandAdaptationSet, locale, contentId, quality string, keys []*widevine.Key) (string, error) {
	if cached := lookupCachedTrack("audio", contentId, quality); cached != "" {
		fmt.Printf("Using cached %s audio\n", trackTitle(locale))
		return cached, nil
	}

	title := "Downloading " + trackTitle(locale) + " audio"
	var file string
	var err error
	if isOnDemand(manifest) {
		file, err = downloadOnDemandAdaptation(title, sets, false, quality, keys)
	} else {
		audioSet := manifest.Period[0].AdaptationSets[1]
		audioBaseUrl, audioRepresentationId := getBaseUrl(audioSet, false, quality)
		if audioBaseUrl == nil {
			return "", fmt.Errorf("failed to get the audio base URL for %s, maybe the audio quality you entered is wrong?", locale)
		}
		file, err = downloadParts(title, audioBaseUrl, audioRepresentationId, audioSet, keys)
	}
	if err != nil {
		return "", err
	}
	storeCachedTrack("audio", contentId, quality, file)
	return file, nil
}

// downloadVideoTrack downloads the video representation into a temporary file.
// The video track is identical across dubs, so it is downloaded only once. When
// caching is enabled a previously downloaded copy is reused.
func downloadVideoTrack(manifest *mpd.MPD, sets []onDemandAdaptationSet, contentId, quality string, keys []*widevine.Key) (string, error) {
	if cached := lookupCachedTrack("video", contentId, quality); cached != "" {
		fmt.Println("Using cached video")
		return cached, nil
	}

	var file string
	var err error
	if isOnDemand(manifest) {
		file, err = downloadOnDemandAdaptation("Downloading video", sets, true, quality, keys)
	} else {
		videoSet := manifest.Period[0].AdaptationSets[0]
		baseUrl, representationId := getBaseUrl(videoSet, true, quality)
		if baseUrl == nil {
			return "", fmt.Errorf("failed to get the video base URL, maybe the video quality you entered is wrong?")
		}
		file, err = downloadParts("Downloading video", baseUrl, representationId, videoSet, keys)
	}
	if err != nil {
		return "", err
	}
	storeCachedTrack("video", contentId, quality, file)
	return file, nil
}

// fetchSubtitle downloads a subtitle/caption file and returns its raw bytes.
func fetchSubtitle(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Origin", "https://static.crunchyroll.com")
	req.Header.Set("Referer", "https://static.crunchyroll.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:147.0) Gecko/20100101 Firefox/147.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func downloadSubs(url, format string) (string, error) {
	body, err := fetchSubtitle(url)
	if err != nil {
		return "", err
	}

	filename := getFilename(nil, format)
	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}
	if _, err = file.Write(body); err != nil {
		file.Close()
		return "", err
	}
	file.Close()

	return filename, nil
}

// buildGuidByLocale maps each available audio locale to its playback GUID. The
// episode's "versions" list is authoritative; the episode's own content ID is
// only a fallback for single-audio content where the API lists no versions.
// Crunchyroll sets "audio_locale" to the preferred language (which may be a dub)
// while the episode ID still points at the original, so mapping audio_locale to
// the content ID directly would resolve the dub to the wrong (original) stream.
func buildGuidByLocale(info EpisodeInfo, baseContentId string) map[string]string {
	guidByLocale := map[string]string{}
	for _, v := range info.EpisodeMetadata.Versions {
		guidByLocale[v.AudioLocale] = v.GUID
	}
	if len(guidByLocale) == 0 && info.EpisodeMetadata.AudioLocale != "" {
		guidByLocale[info.EpisodeMetadata.AudioLocale] = baseContentId
	}
	return guidByLocale
}

// mergeSubtitleAndCaptions combines the subtitles and captions offered by every
// audio version of an episode. Subtitles (translation scripts) are usually
// identical across versions, but captions (closed captions) transcribe a
// specific dub and only appear on that version's playback. Taking the first
// non-nil entry per locale preserves the union without duplication.
func mergeSubtitleAndCaptions(episodes []Episode) (subtitles, captions map[string]*Subtitle) {
	subtitles = map[string]*Subtitle{}
	captions = map[string]*Subtitle{}
	for _, ep := range episodes {
		for locale, sub := range ep.Subtitles {
			if sub != nil && subtitles[locale] == nil {
				subtitles[locale] = sub
			}
		}
		for locale, cc := range ep.Captions {
			if cc != nil && captions[locale] == nil {
				captions[locale] = cc
			}
		}
	}
	return subtitles, captions
}

// filterAvailableLangs drops subtitle/caption locales that the episode does not
// offer, warning about each one instead of aborting the whole download. Subtitles
// are optional, so a missing locale (e.g. the default "en-US" on a movie) should
// not prevent the video and audio from being saved.
func filterAvailableLangs(langs []string, available map[string]*Subtitle, kind string, episode int) []string {
	var filtered []string
	for _, locale := range langs {
		if available[locale] == nil {
			fmt.Printf("! %s locale %s is not available for episode %v, skipping it.\n", kind, locale, episode)
			continue
		}
		filtered = append(filtered, locale)
	}
	return filtered
}

func downloadEpisode(baseContentId string, info EpisodeInfo, audioLangs, subsLangs, ccLangs []string, videoQuality, audioQuality *string) (err error) {
	mode := currentMode()

	// Subtitles-only mode downloads no audio, so it looks at every dub version
	// to discover all subtitle and caption locales.
	if mode == modeSubsOnly {
		audioLangs = []string{"all"}
	}

	cleanSeriesTitle := sanitizeFilename(info.EpisodeMetadata.SeriesTitle)
	cleanEpisodeTitle := sanitizeFilename(info.Title)

	if _, err := os.Stat(cleanSeriesTitle); err != nil {
		_ = os.MkdirAll(cleanSeriesTitle, 0777)
	}

	baseName := episodeBaseName(info)
	seasonFolder := seasonFolderName(info.EpisodeMetadata.SeasonNumber)

	outputFile := ""
	if mode == modeFull {
		outputFile = filepath.Join(cleanSeriesTitle, fmt.Sprintf("%s S%02dE%02d - %s [%s].mkv",
			cleanSeriesTitle,
			info.EpisodeMetadata.SeasonNumber,
			info.EpisodeMetadata.EpisodeNumber,
			cleanEpisodeTitle,
			*videoQuality,
		))

		if _, statErr := os.Stat(outputFile); statErr == nil {
			fmt.Printf("Episode %v is already downloaded, skipping...\n", info.EpisodeMetadata.EpisodeNumber)
			return
		}
	}

	// Resolve each requested audio locale to its version GUID. Each dub is a
	// separate playback stream with its own manifest, token and Widevine keys.
	guidByLocale := buildGuidByLocale(info, baseContentId)

	if len(audioLangs) == 1 && audioLangs[0] == "all" {
		audioLangs = make([]string, 0, len(guidByLocale))
		if primaryLocale := info.EpisodeMetadata.AudioLocale; primaryLocale != "" {
			if _, ok := guidByLocale[primaryLocale]; ok {
				audioLangs = append(audioLangs, primaryLocale)
			}
		}
		for locale := range guidByLocale {
			if locale != info.EpisodeMetadata.AudioLocale {
				audioLangs = append(audioLangs, locale)
			}
		}
		if len(audioLangs) > 1 {
			sort.Strings(audioLangs[1:])
		}
	}

	type audioVersion struct {
		locale    string
		contentId string
	}
	var versions []audioVersion
	for _, locale := range audioLangs {
		guid, ok := guidByLocale[locale]
		if !ok {
			fmt.Printf("! Audio locale %s is not available for episode %v, skipping it.\n", locale, info.EpisodeMetadata.EpisodeNumber)
			continue
		}
		versions = append(versions, audioVersion{locale: locale, contentId: guid})
	}
	if len(versions) == 0 {
		fmt.Printf("! None of the requested audio locales are available for episode %v, aborting this episode.\n", info.EpisodeMetadata.EpisodeNumber)
		return
	}

	fmt.Printf("Downloading: %s (S%02vE%02v) from %s\n", info.Title, info.EpisodeMetadata.SeasonNumber, info.EpisodeMetadata.EpisodeNumber, info.EpisodeMetadata.SeriesTitle)

	// Space this download out from the previous one, if a delay is configured.
	// Applied here rather than around the whole function so that episodes
	// already on disk (returned above) don't burn the wait for nothing.
	backoff.wait()

	// Playback streams are acquired one version at a time and released as soon
	// as that version is finished with. Crunchyroll counts every open playback
	// token against the account's concurrent-stream limit, so fetching all the
	// dubs up front (which -subs-only did to discover per-dub captions) opened
	// one stream per dub and tripped TOO_MANY_ACTIVE_STREAMS.
	streams := newStreamTracker()
	setCurrentStreams(streams)
	defer func() {
		// Start the backoff clock as soon as this download is over, success or
		// not, so a failed/rate-limited attempt doesn't get retried immediately.
		backoff.done()

		print("Cleaning up...\n")

		streams.releaseAll()
		setCurrentStreams(nil)

		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("%v", r)
			}
			if *debug {
				fmt.Printf("Recovered from error: %v\n%s\n", r, runtimedebug.Stack())
			} else {
				fmt.Printf("Recovered from error: %v\n", r)
			}
		}
	}()

	// Audio-only mode knows its track list up front; the other modes have to
	// read a playback response before they can tell which subtitles exist.
	if mode == modeAudioOnly {
		fmt.Printf("Audio locales: %s\n", strings.Join(audioLangs, ", "))
	}

	// episodes holds the playback metadata of every version whose stream was
	// read, so the subtitles and captions of all dubs can still be merged once
	// the streams have been released.
	var episodes []Episode
	audioTracks := make([]mediaTrack, len(versions))
	var videoFile string

	for i, version := range versions {
		// In audio-only mode a track already on disk needs no re-download.
		if mode == modeAudioOnly {
			target := filepath.Join(cleanSeriesTitle, seasonFolder, version.locale, baseName+"."+audioTrackExt)
			if fileExists(target) {
				fmt.Printf("Audio track %s is already downloaded, skipping...\n", trackTitle(version.locale))
				continue
			}
		}

		episode, err := acquirePlayback(version.contentId)
		if err != nil {
			panic(err)
		}
		streams.add(version.contentId, episode.Token)
		episodes = append(episodes, episode)

		if mode != modeSubsOnly {
			manifest, body, err := parseManifest(episode.ManifestURL)
			if err != nil {
				panic(fmt.Errorf("parsing manifest for %s: %w", version.locale, err))
			}
			pssh := getPssh(manifest)
			if pssh == nil {
				panic(fmt.Errorf("PSSH not found for %s", version.locale))
			}
			keys, err := getLicense(*pssh, version.contentId, episode.Token)
			if err != nil {
				panic(fmt.Errorf("getLicense for %s: %w", version.locale, err))
			}

			var sets []onDemandAdaptationSet
			if isOnDemand(manifest) {
				if sets, err = parseOnDemand(body); err != nil {
					panic(err)
				}
			}

			if i == 0 && mode == modeFull {
				// The video and the first audio track share this version's keys,
				// so they download concurrently with each other.
				type trackResult struct {
					file string
					err  error
				}
				videoCh := make(chan trackResult, 1)
				go func() {
					// The video track is the same for every dub, so its cache key
					// uses the episode's base ID rather than the version's, keeping
					// the key stable regardless of which audio languages are asked
					// for.
					file, err := downloadVideoTrack(manifest, sets, baseContentId, *videoQuality, keys)
					videoCh <- trackResult{file, err}
				}()

				audioFile, err := downloadAudioTrack(manifest, sets, version.locale, version.contentId, *audioQuality, keys)
				if err != nil {
					// Join the video download before unwinding so its progress
					// bar can't race the cleanup.
					<-videoCh
					panic(err)
				}
				audioTracks[i] = mediaTrack{file: audioFile, locale: version.locale}

				video := <-videoCh
				if video.err != nil {
					panic(video.err)
				}
				videoFile = video.file
			} else {
				audioFile, err := downloadAudioTrack(manifest, sets, version.locale, version.contentId, *audioQuality, keys)
				if err != nil {
					panic(err)
				}
				audioTracks[i] = mediaTrack{file: audioFile, locale: version.locale}
			}
		}

		// Free this version's stream before asking for the next one, so the
		// account never has more than one playback open at a time.
		streams.release(version.contentId)
	}

	var subJobs []subJob
	if mode != modeAudioOnly {
		// Merge subtitles and captions across versions. Subtitles (translation
		// scripts) are usually identical across versions, while captions are the
		// per-dub transcriptions that only exist on their own version.
		subtitles, captions := mergeSubtitleAndCaptions(episodes)

		if len(subsLangs) == 1 && subsLangs[0] == "all" {
			subsLangs = make([]string, 0, len(subtitles))
			for locale, sub := range subtitles {
				if sub != nil && sub.URL != "" {
					subsLangs = append(subsLangs, locale)
				}
			}
			sort.Strings(subsLangs)
		}
		if len(ccLangs) == 1 && ccLangs[0] == "all" {
			ccLangs = make([]string, 0, len(captions))
			for locale, cc := range captions {
				if cc != nil && cc.URL != "" {
					ccLangs = append(ccLangs, locale)
				}
			}
			sort.Strings(ccLangs)
		}

		fmt.Printf("Audio locales: %s | Subtitle locales: %s | CC locales: %s\n",
			strings.Join(audioLangs, ", "), strings.Join(subsLangs, ", "), strings.Join(ccLangs, ", "))

		subsLangs = filterAvailableLangs(subsLangs, subtitles, "Subtitle", info.EpisodeMetadata.EpisodeNumber)
		ccLangs = filterAvailableLangs(ccLangs, captions, "Closed caption", info.EpisodeMetadata.EpisodeNumber)

		// Build the list of subtitle and caption downloads.
		for _, locale := range subsLangs {
			sub := subtitles[locale]
			subJobs = append(subJobs, subJob{url: sub.URL, format: sub.Format, locale: locale})
		}
		for _, locale := range ccLangs {
			cc := captions[locale]
			subJobs = append(subJobs, subJob{url: cc.URL, format: cc.Format, locale: locale, isCC: true})
		}
	}

	// Subtitles-only mode writes each file straight into its per-language
	// folder and never touches the video or audio streams.
	if mode == modeSubsOnly {
		return downloadSubsOnly(cleanSeriesTitle, seasonFolder, baseName, subJobs)
	}

	// Subtitles are fetched from the CDN over their own URLs rather than through
	// the playback stream, so they still download concurrently. Only the
	// playback streams themselves are serialized.
	subTracks := make([]mediaTrack, len(subJobs))

	var (
		wg       sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)
	fail := func(err error) {
		errMu.Lock()
		if firstErr == nil && err != nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	for j, job := range subJobs {
		wg.Add(1)
		go func(j int, job subJob) {
			defer wg.Done()
			file, err := downloadSubs(job.url, job.format)
			if err != nil {
				fail(fmt.Errorf("downloading subtitles for %s: %w", trackTitle(job.locale), err))
				return
			}
			subTracks[j] = mediaTrack{file: file, locale: job.locale, format: job.format, isCC: job.isCC}
		}(j, job)
	}

	wg.Wait()
	if firstErr != nil {
		panic(firstErr)
	}

	// Audio-only mode writes each language's track to its own folder instead of
	// merging, so no video or subtitle streams are involved.
	if mode == modeAudioOnly {
		for _, track := range audioTracks {
			if track.file == "" {
				continue // skipped above: already on disk
			}
			if _, err := sidecarDir(cleanSeriesTitle, seasonFolder, track.locale); err != nil {
				panic(err)
			}
			target := filepath.Join(cleanSeriesTitle, seasonFolder, track.locale, baseName+"."+audioTrackExt)
			if err := moveFile(track.file, target); err != nil {
				panic(fmt.Errorf("writing %s: %w", target, err))
			}
			fmt.Printf("Saved %s audio to %s\n", trackTitle(track.locale), target)
		}
		fmt.Println("Audio download finished!")
		return nil
	}

	if len(subTracks) > 0 {
		fmt.Println("Downloaded subtitles!")
	}

	mergeEverything(videoFile, audioTracks, subTracks, outputFile, info)
	return nil
}

// downloadSubsOnly writes each subtitle/caption file straight into its
// per-language folder: <series>/<season>/<locale>/<SxxEyy - Title>.<format>.
func downloadSubsOnly(series, seasonFolder, baseName string, jobs []subJob) error {
	for _, job := range jobs {
		kind := "Subtitle"
		if job.isCC {
			kind = "Closed caption"
		}
		dir, err := sidecarDir(series, seasonFolder, job.locale)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, sidecarName(baseName, job.format, job.isCC))
		if fileExists(target) {
			fmt.Printf("%s %s is already downloaded, skipping...\n", kind, trackTitle(job.locale))
			continue
		}

		body, err := fetchSubtitle(job.url)
		if err != nil {
			return fmt.Errorf("downloading %s for %s: %w", strings.ToLower(kind), trackTitle(job.locale), err)
		}
		if err := os.WriteFile(target, body, 0666); err != nil {
			return fmt.Errorf("writing %s: %w", target, err)
		}
		fmt.Printf("Saved %s %s to %s\n", trackTitle(job.locale), strings.ToLower(kind), target)
	}
	fmt.Println("Subtitle download finished!")
	return nil
}

// downloadEpisodeWithRetry runs downloadEpisode, retrying the same episode
// with a growing delay whenever Crunchyroll rate-limits the account, instead
// of moving on to the next episode and immediately tripping the same rate
// limit again.
func downloadEpisodeWithRetry(baseContentId string, info EpisodeInfo, audioLangs, subsLangs, ccLangs []string, videoQuality, audioQuality *string) {
	for {
		err := downloadEpisode(baseContentId, info, audioLangs, subsLangs, ccLangs, videoQuality, audioQuality)
		if err == nil {
			backoff.resetRateLimit()
			return
		}
		if !errors.Is(err, ErrRateLimited) {
			return
		}

		wait := backoff.rateLimitWait()
		retryAt := time.Now().Add(wait)
		fmt.Printf("Retrying this episode in %s [%s]...\n", wait.Round(time.Second), retryAt.Local().Format(time.Kitchen))
		time.Sleep(wait)
	}
}

func downloadSeason(videoQuality, audioQuality *string, audioLangs, subsLangs, ccLangs []string, episodes []SeasonEpisode) {
	fmt.Printf("Downloading season %v of %s (%v episodes)\n\n", episodes[0].SeasonNumber, episodes[0].SeriesTitle, len(episodes))

	for _, episode := range episodes {
		info := EpisodeInfo{
			EpisodeMetadata: EpisodeMetadata{
				SeriesTitle:        episode.SeriesTitle,
				SeasonNumber:       episode.SeasonNumber,
				EpisodeNumber:      episode.EpisodeNumber,
				AudioLocale:        episode.AudioLocale,
				Versions:           episode.Versions,
				AvailabilityStarts: episode.AvailabilityStarts,
			},
			Title: episode.Title,
		}

		downloadEpisodeWithRetry(episode.ID, info, audioLangs, subsLangs, ccLangs, videoQuality, audioQuality)
	}
}
