package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// streamTracker remembers the playback tokens that are currently open so they
// can be released on the way out, including when the program is interrupted.
//
// Releasing matters: Crunchyroll counts every open playback token against the
// account's concurrent-stream limit until it is deleted. Tokens stranded by an
// interrupted run are the usual reason a later run fails with
// TOO_MANY_ACTIVE_STREAMS even though nothing is playing.
type streamTracker struct {
	mu      sync.Mutex
	streams map[string]string // content ID -> playback token

	// delete releases one token server-side. It is a field so tests can stand
	// in for deleteStream instead of making a real request.
	delete func(contentId, token string) error
}

func newStreamTracker() *streamTracker {
	return &streamTracker{streams: map[string]string{}, delete: deleteStream}
}

// add records a playback token so it can be released later.
func (s *streamTracker) add(contentId, token string) {
	s.mu.Lock()
	s.streams[contentId] = token
	s.mu.Unlock()
}

// release deletes one playback token and forgets it. Releasing an unknown
// content ID is a no-op, so a caller can release unconditionally.
func (s *streamTracker) release(contentId string) {
	s.mu.Lock()
	token, ok := s.streams[contentId]
	delete(s.streams, contentId)
	s.mu.Unlock()
	if ok {
		s.releaseToken(contentId, token)
	}
}

// releaseAll deletes every recorded playback token. Safe to call more than once.
func (s *streamTracker) releaseAll() {
	s.mu.Lock()
	streams := s.streams
	s.streams = map[string]string{}
	s.mu.Unlock()
	for contentId, token := range streams {
		s.releaseToken(contentId, token)
	}
}

// releaseToken reports rather than returns a failure: this runs during cleanup
// and teardown, where there is nothing useful left to do with the error.
func (s *streamTracker) releaseToken(contentId, token string) {
	if err := s.delete(contentId, token); err != nil {
		fmt.Printf("Failed to remove the player stream for %s: %v\n", contentId, err)
	}
}

// currentStreams is the tracker for the episode being downloaded, if any. It is
// what the interrupt handler releases so pressing Ctrl+C doesn't strand a
// stream and leave the account at its limit for the next run.
var (
	currentStreamsMu sync.Mutex
	currentStreams   *streamTracker
)

func setCurrentStreams(s *streamTracker) {
	currentStreamsMu.Lock()
	currentStreams = s
	currentStreamsMu.Unlock()
}

// handleInterrupt releases the playback streams of the episode in flight and
// exits when the process is interrupted, instead of leaving them registered
// server-side until they time out.
func handleInterrupt() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-ch
		fmt.Printf("\nReceived %s, releasing active player streams...\n", sig)

		currentStreamsMu.Lock()
		s := currentStreams
		currentStreamsMu.Unlock()
		if s != nil {
			s.releaseAll()
		}
		os.Exit(1)
	}()
}

// getEpisodeFunc and sleep are seams for tests: the first lets acquirePlayback
// run against a fake playback endpoint, the second keeps retry tests from
// actually waiting.
var (
	getEpisodeFunc = getEpisode
	sleep          = time.Sleep
)

// maxStreamAttempts bounds how many times a playback request is retried after
// Crunchyroll reports the account is already at its concurrent-stream limit.
const maxStreamAttempts = 5

// acquirePlayback requests an episode's playback, waiting and retrying when
// Crunchyroll reports that the account already has too many active streams.
// Streams stranded by an interrupted run (or held by another device) keep
// counting against the limit until they expire, so a short wait usually clears
// it without the user having to do anything.
func acquirePlayback(contentId string) (Episode, error) {
	var hintPrinted bool
	for attempt := 0; ; attempt++ {
		episode, err := getEpisodeFunc(contentId)
		if err == nil {
			backoff.resetStreamLimit()
			return episode, nil
		}
		if !errors.Is(err, ErrTooManyStreams) || attempt >= maxStreamAttempts-1 {
			return Episode{}, err
		}

		wait := backoff.streamLimitWait()
		fmt.Printf("This account is already at Crunchyroll's concurrent-stream limit. Waiting %s before retrying (attempt %d/%d)...\n",
			wait.Round(time.Second), attempt+2, maxStreamAttempts)
		if !hintPrinted {
			fmt.Println("Streams left over from an interrupted run count until they expire: stop playback on other devices/tabs, or leave the downloader idle for a few minutes, then try again.")
			hintPrinted = true
		}
		sleep(wait)
	}
}
