package main

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestPlaybackErrorClassification(t *testing.T) {
	tests := []struct {
		name    string
		message EpisodeError
		reason  string
		want    error // sentinel the error must wrap, or nil for a plain error
		wantNil bool
	}{
		{name: "no error", wantNil: true},
		{name: "rate limit", message: "4294", reason: "Too many requests", want: ErrRateLimited},
		{name: "too many active streams", message: "TOO_MANY_ACTIVE_STREAMS", want: ErrTooManyStreams},
		{name: "too many active streams with reason", message: "TOO_MANY_ACTIVE_STREAMS", reason: "Stream limit reached", want: ErrTooManyStreams},
		{name: "lowercase stream error", message: "too_many_active_streams", want: ErrTooManyStreams},
		{name: "other playback error", message: "region locked"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := playbackError(tc.message, tc.reason)
			if tc.wantNil {
				if err != nil {
					t.Fatalf("playbackError(%q, %q) = %v, want nil", tc.message, tc.reason, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("playbackError(%q, %q) = nil, want an error", tc.message, tc.reason)
			}
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("playbackError(%q, %q) = %v, want it to wrap %v", tc.message, tc.reason, err, tc.want)
				}
				return
			}
			// A plain failure must not be mistaken for one of the retryable ones.
			if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrTooManyStreams) {
				t.Fatalf("playbackError(%q, %q) = %v, want a plain error", tc.message, tc.reason, err)
			}
		})
	}
}

func TestEpisodeUnmarshalErrorField(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		wantErr    string
		wantReason string
	}{
		{"string error", `{"error":"region locked"}`, "region locked", ""},
		{"false", `{"error":false}`, "", ""},
		{"null", `{"error":null}`, "", ""},
		{"zero number", `{"error":0}`, "", ""},
		{"nonzero number", `{"error":403}`, "403", ""},
		{"true", `{"error":true}`, "true", ""},
		{"missing", `{}`, "", ""},
		{"rate limit", `{"error":4294,"reason":"Too many requests"}`, "4294", "Too many requests"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ep Episode
			if err := json.Unmarshal([]byte(tc.json), &ep); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if string(ep.Error) != tc.wantErr {
				t.Fatalf("Error = %q, want %q", ep.Error, tc.wantErr)
			}
			if ep.Reason != tc.wantReason {
				t.Fatalf("Reason = %q, want %q", ep.Reason, tc.wantReason)
			}
		})
	}
}
