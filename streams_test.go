package main

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestStreamTrackerReleaseOnlyDeletesThatStream(t *testing.T) {
	var deleted []string
	tr := newStreamTracker()
	tr.delete = func(contentId, token string) error {
		deleted = append(deleted, contentId+"/"+token)
		return nil
	}

	tr.add("a", "tok-a")
	tr.add("b", "tok-b")

	tr.release("a")
	if len(deleted) != 1 || deleted[0] != "a/tok-a" {
		t.Fatalf("release(a) deleted %v, want [a/tok-a]", deleted)
	}

	// Releasing the same content ID again must not repeat the request.
	tr.release("a")
	if len(deleted) != 1 {
		t.Fatalf("releasing a twice made %d calls, want 1", len(deleted))
	}

	tr.releaseAll()
	if len(deleted) != 2 || deleted[1] != "b/tok-b" {
		t.Fatalf("releaseAll() deleted %v, want b/tok-b as the only remaining stream", deleted)
	}

	// A second releaseAll has nothing left to release.
	tr.releaseAll()
	if len(deleted) != 2 {
		t.Fatalf("second releaseAll() made %d calls, want no further calls", len(deleted))
	}
}

func TestStreamTrackerReleaseUnknownIsNoop(t *testing.T) {
	tr := newStreamTracker()
	tr.delete = func(string, string) error {
		t.Fatal("delete called for a content ID that was never added")
		return nil
	}
	tr.release("missing")
}

// stubPlayback swaps the playback endpoint and the wait for fakes, so the retry
// loop can be exercised without network access or real delays.
func stubPlayback(t *testing.T, fake func(string) (Episode, error)) *[]time.Duration {
	t.Helper()
	origGet, origSleep, origBackoff := getEpisodeFunc, sleep, backoff
	t.Cleanup(func() { getEpisodeFunc, sleep, backoff = origGet, origSleep, origBackoff })

	var slept []time.Duration
	getEpisodeFunc = fake
	sleep = func(d time.Duration) { slept = append(slept, d) }
	backoff = newDownloadBackoff(0)
	return &slept
}

func streamLimitError() error {
	return fmt.Errorf("playback error: TOO_MANY_ACTIVE_STREAMS: %w", ErrTooManyStreams)
}

func TestAcquirePlaybackRetriesStreamLimit(t *testing.T) {
	attempts := 0
	slept := stubPlayback(t, func(string) (Episode, error) {
		attempts++
		if attempts < 3 {
			return Episode{}, streamLimitError()
		}
		return Episode{Token: "tok"}, nil
	})

	ep, err := acquirePlayback("id")
	if err != nil {
		t.Fatalf("acquirePlayback() = %v, want nil", err)
	}
	if ep.Token != "tok" {
		t.Fatalf("Token = %q, want %q", ep.Token, "tok")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(*slept) != 2 {
		t.Fatalf("waited %d times, want 2", len(*slept))
	}
	// The wait doubles after each consecutive hit.
	if (*slept)[0] != defaultStreamWait || (*slept)[1] != 2*defaultStreamWait {
		t.Fatalf("waits = %v, want [%v %v]", *slept, defaultStreamWait, 2*defaultStreamWait)
	}
}

func TestAcquirePlaybackDoesNotRetryOtherErrors(t *testing.T) {
	attempts := 0
	slept := stubPlayback(t, func(string) (Episode, error) {
		attempts++
		return Episode{}, errors.New("region locked")
	})

	if _, err := acquirePlayback("id"); err == nil {
		t.Fatal("acquirePlayback() = nil, want the playback error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (only the stream limit is retried)", attempts)
	}
	if len(*slept) != 0 {
		t.Fatalf("waited %v, want no wait", *slept)
	}
}

func TestAcquirePlaybackGivesUpAfterMaxAttempts(t *testing.T) {
	attempts := 0
	stubPlayback(t, func(string) (Episode, error) {
		attempts++
		return Episode{}, streamLimitError()
	})

	if _, err := acquirePlayback("id"); !errors.Is(err, ErrTooManyStreams) {
		t.Fatalf("acquirePlayback() = %v, want ErrTooManyStreams", err)
	}
	if attempts != maxStreamAttempts {
		t.Fatalf("attempts = %d, want %d", attempts, maxStreamAttempts)
	}
}
