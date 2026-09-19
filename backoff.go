package main

import (
	"fmt"
	"sync"
	"time"
)

// downloadBackoff enforces a minimum gap between the end of one episode
// download and the start of the next, to help avoid tripping Crunchyroll's
// rate limiting on accounts that download many episodes in a row. now and
// sleep are overridable so tests don't have to wait on the real clock.
type downloadBackoff struct {
	delay time.Duration
	now   func() time.Time
	sleep func(time.Duration)

	mu            sync.Mutex
	started       bool
	last          time.Time
	rateLimitHits int
	streamHits    int
}

// defaultRateLimitWait is the starting retry delay for a rate-limited episode
// when no -download-delay is configured.
const defaultRateLimitWait = time.Minute

// maxRateLimitWait caps rateLimitWait's exponential growth so a long streak of
// consecutive rate-limit hits doesn't produce absurd delays.
const maxRateLimitWait = 30 * time.Minute

// defaultStreamWait is the starting retry delay for a playback request that hit
// the account's concurrent-stream limit. A stream left behind by an interrupted
// run usually expires within a couple of minutes, so this stays short.
const defaultStreamWait = 10 * time.Second

// maxStreamWait caps streamLimitWait's exponential growth.
const maxStreamWait = time.Minute

func newDownloadBackoff(delay time.Duration) *downloadBackoff {
	return &downloadBackoff{delay: delay, now: time.Now, sleep: time.Sleep}
}

// wait blocks until delay has elapsed since the matching done call for the
// previous download. It is a no-op before the first download, when b is nil,
// or when no delay is configured.
func (b *downloadBackoff) wait() {
	if b == nil || b.delay <= 0 {
		return
	}

	b.mu.Lock()
	if !b.started {
		b.mu.Unlock()
		return
	}
	remaining := b.delay - b.now().Sub(b.last)
	b.mu.Unlock()

	if remaining > 0 {
		fmt.Printf("Waiting %s before the next download (rate-limit backoff)...\n", remaining.Round(time.Second))
		b.sleep(remaining)
	}
}

// done marks a download as finished, starting the clock that the next wait
// call measures against.
func (b *downloadBackoff) done() {
	if b == nil || b.delay <= 0 {
		return
	}
	b.mu.Lock()
	b.last = b.now()
	b.started = true
	b.mu.Unlock()
}

// rateLimitWait returns how long to wait before retrying an episode that was
// just rate-limited by Crunchyroll, doubling with each consecutive hit (since
// the last resetRateLimit) up to maxRateLimitWait. Safe to call on a nil
// backoff, so it works whether or not -download-delay is configured.
func (b *downloadBackoff) rateLimitWait() time.Duration {
	base := defaultRateLimitWait
	if b != nil && b.delay > base {
		base = b.delay
	}

	var hits int
	if b != nil {
		b.mu.Lock()
		hits = b.rateLimitHits
		b.rateLimitHits++
		b.mu.Unlock()
	}

	wait := base
	for i := 0; i < hits && wait < maxRateLimitWait; i++ {
		wait *= 2
	}
	if wait > maxRateLimitWait {
		wait = maxRateLimitWait
	}
	return wait
}

// resetRateLimit clears the consecutive-hit counter after a successful
// download. Safe to call on a nil backoff.
func (b *downloadBackoff) resetRateLimit() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.rateLimitHits = 0
	b.mu.Unlock()
}

// streamLimitWait returns how long to wait before retrying a playback request
// that Crunchyroll rejected because the account already had too many active
// streams, doubling with each consecutive hit (since the last resetStreamLimit)
// up to maxStreamWait. Safe to call on a nil backoff.
func (b *downloadBackoff) streamLimitWait() time.Duration {
	var hits int
	if b != nil {
		b.mu.Lock()
		hits = b.streamHits
		b.streamHits++
		b.mu.Unlock()
	}

	wait := defaultStreamWait
	for i := 0; i < hits && wait < maxStreamWait; i++ {
		wait *= 2
	}
	if wait > maxStreamWait {
		wait = maxStreamWait
	}
	return wait
}

// resetStreamLimit clears the consecutive stream-limit counter after a playback
// request succeeds. Safe to call on a nil backoff.
func (b *downloadBackoff) resetStreamLimit() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.streamHits = 0
	b.mu.Unlock()
}
