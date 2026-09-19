package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

var (
	token         = ""
	audioLang     = flag.String("audio-lang", "ja-JP", "Audio language(s), comma-separated for multiple (e.g. \"ja-JP,en-US\") or \"all\" for every available language. First is the default track")
	subtitlesLang = flag.String("subs-lang", "en-US", "Subtitle language(s), comma-separated for multiple (e.g. \"en-US,es-419\") or \"all\" for every available subtitle. First is the default track")
	ccLang        = flag.String("cc-lang", "", "Closed caption language(s), comma-separated for multiple (e.g. \"en-US\") or \"all\" for every available caption. Downloaded in addition to --subs-lang, not instead of it")
	videoQuality  = flag.String("video-quality", "1080p", "Video quality")
	audioQuality  = flag.String("audio-quality", "192k", "Audio quality")
	seasonNumber  = flag.Int("season", 0, "Season number. Not used if an episode link is entered")
	etpRt         = flag.String("etp-rt", "", "The \"etp_rt\" cookie value of your account")
	debug         = flag.Bool("debug-manifest", false, "Log raw episode playback JSON and manifest XML")
	downloadDelay = flag.Duration("download-delay", 0, "Minimum delay between episode downloads, to help avoid Crunchyroll's rate limiting (e.g. \"30s\", \"2m\")")
	allAudioSubs  = flag.Bool("all-audio-subs", false, "Download every available audio language together with every available subtitle and closed caption")
	audioOnly     = flag.Bool("audio-only", false, "Download audio only (no video or subtitles), one file per language in its own folder")
	subsOnly      = flag.Bool("subs-only", false, "Download subtitles only (no video or audio), one file per language in its own folder")
	enableCache   = flag.Bool("cache", false, "Cache decrypted video and audio tracks so re-running an episode doesn't download them again")
)

// backoff spaces out consecutive episode downloads by *downloadDelay. It is
// initialized in main() once flags are parsed, and is a no-op (including when
// left nil) whenever no delay is configured.
var backoff *downloadBackoff

// flagAliases maps alternate flag spellings to their canonical name. They are
// resolved by normalizeArgs before flag.Parse, so aliases never show up in the
// -h usage output.
var flagAliases = map[string]string{
	"sub-only": "subs-only",
}

// normalizeArgs rewrites alias flags to their canonical spelling before
// flag.Parse sees them. Both the "-name" and "--name" forms are handled, with
// or without an "=value" suffix. Arguments after a bare "--" are left alone,
// since those are positional by convention.
func normalizeArgs(args []string) {
	for i, arg := range args {
		if arg == "--" {
			return
		}
		if len(arg) == 0 || arg[0] != '-' {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		canonical, ok := flagAliases[name]
		if !ok {
			continue
		}
		if hasValue {
			args[i] = "--" + canonical + "=" + value
		} else {
			args[i] = "--" + canonical
		}
	}
}

// primaryLocale returns the first requested locale, substituting fallback when
// the list is empty or selects every language. "all" is a downloader keyword,
// not a real locale, so it must never be sent to the listing API (which still
// returns every dub version per episode, keeping the other locales resolvable).
func primaryLocale(langs []string, fallback string) string {
	if len(langs) == 0 || langs[0] == "all" {
		return fallback
	}
	return langs[0]
}

// parseLangs splits a comma-separated locale list, trimming spaces and dropping
// empties.
func parseLangs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseUrl extracts the content type ("watch" or "series") and content ID from a
// Crunchyroll URL, tolerating an optional locale prefix such as "/fr/". The
// content type and ID may appear at any position after the host.
func parseUrl(url string) (contentType, contentId string) {
	parts := strings.Split(strings.TrimRight(url, "/"), "/")
	for i, p := range parts {
		if (p == "watch" || p == "series") && i+1 < len(parts) {
			return p, parts[i+1]
		}
	}
	return "", ""
}

func processUrl(url string) {
	contentType, contentId := parseUrl(url)
	if contentType == "" || contentId == "" {
		fmt.Printf("Invalid URL (must be /watch/ or /series/): %s\n", url)
		return
	}

	if *audioOnly && *subsOnly {
		fmt.Println("-audio-only and -subs-only cannot be used together.")
		return
	}

	audioLangs := parseLangs(*audioLang)
	if len(audioLangs) == 0 {
		audioLangs = []string{"ja-JP"}
	}
	subsLangs := parseLangs(*subtitlesLang)
	ccLangs := parseLangs(*ccLang)

	// -all-audio-subs is shorthand for grabbing everything Crunchyroll offers.
	if *allAudioSubs {
		audioLangs = []string{"all"}
		subsLangs = []string{"all"}
		ccLangs = []string{"all"}
	}

	// The season/series API endpoints take a single preferred locale; use the
	// primary (first) requested one. All dub versions are still listed per
	// episode, so the other languages remain resolvable.
	primaryAudio := primaryLocale(audioLangs, "ja-JP")
	primarySubs := primaryLocale(subsLangs, "en-US")

	if contentType == "watch" {
		info := getEpisodeInfo(contentId)
		downloadEpisodeWithRetry(contentId, info, audioLangs, subsLangs, ccLangs, videoQuality, audioQuality)
	} else {
		seasons := getSeasons(contentId, primaryAudio, primarySubs)

		if *seasonNumber != 0 {
			var seasonId string
			for _, season := range seasons {
				if season.SeasonNumber == *seasonNumber {
					seasonId = season.ID
					break
				}
			}
			if seasonId == "" {
				fmt.Printf("This anime has no season %v!\n", *seasonNumber)
				return
			}

			episodes := getSeasonEpisodes(seasonId, primaryAudio, primarySubs)
			downloadSeason(videoQuality, audioQuality, audioLangs, subsLangs, ccLangs, episodes)
		} else {
			print("No season number specified, downloading all seasons...\n")

			for _, season := range seasons {
				episodes := getSeasonEpisodes(season.ID, primaryAudio, primarySubs)
				downloadSeason(videoQuality, audioQuality, audioLangs, subsLangs, ccLangs, episodes)
			}
		}
	}
}

func main() {
	url := flag.String("url", "", "URL of the episode/season to download")
	urlsFile := flag.String("file", "", "Path to a text file with one URL per line")
	// Resolve hidden flag aliases (e.g. -sub-only -> -subs-only) first.
	normalizeArgs(os.Args)
	flag.Parse()

	if *url == "" && *urlsFile == "" {
		flag.Usage()
		os.Exit(1)
	}

	if *etpRt == "" {
		fmt.Println("You must specify the \"-etp-rt\" option!\n- Open Crunchyroll on your browser and log in.\n- Open developer tools (Ctrl+Shift+I), go to \"Application\", and then \"Cookies\".\n- The value of the \"ept_rt\" cookie is what you need to input into this option.")
		os.Exit(1)
	}

	token = GetAccessToken(*etpRt)
	backoff = newDownloadBackoff(*downloadDelay)
	// Release the playback streams of an in-flight episode on Ctrl+C, so an
	// interrupted run can't leave the account at its concurrent-stream limit.
	handleInterrupt()

	if *urlsFile != "" {
		file, err := os.Open(*urlsFile)
		if err != nil {
			fmt.Printf("Failed to open URLs file: %s\n", err)
			os.Exit(1)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		var urls []string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && strings.HasPrefix(line, "http") {
				urls = append(urls, line)
			}
		}

		fmt.Printf("Found %d URLs to download\n\n", len(urls))
		for i, u := range urls {
			fmt.Printf("=== [%d/%d] %s ===\n", i+1, len(urls), u)
			processUrl(u)
			fmt.Println()
		}
	} else {
		processUrl(*url)
	}
}
