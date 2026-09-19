package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeasonFolderName(t *testing.T) {
	cases := map[int]string{
		0:   "Season 00",
		1:   "Season 01",
		12:  "Season 12",
		100: "Season 100",
	}
	for in, want := range cases {
		if got := seasonFolderName(in); got != want {
			t.Fatalf("seasonFolderName(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestEpisodeBaseName(t *testing.T) {
	info := EpisodeInfo{
		Title: "Da:wn/Test",
		EpisodeMetadata: EpisodeMetadata{
			SeasonNumber:  1,
			EpisodeNumber: 2,
		},
	}
	if got, want := episodeBaseName(info), "S01E02 - Da_wn_Test"; got != want {
		t.Fatalf("episodeBaseName() = %q, want %q", got, want)
	}
}

func TestSidecarName(t *testing.T) {
	if got, want := sidecarName("S01E01 - Foo", "ass", false), "S01E01 - Foo.ass"; got != want {
		t.Fatalf("sidecarName() = %q, want %q", got, want)
	}
	// Closed captions get tagged so they can't collide with a same-format
	// subtitle for the same locale.
	if got, want := sidecarName("S01E01 - Foo", "vtt", true), "S01E01 - Foo [CC].vtt"; got != want {
		t.Fatalf("sidecarName() = %q, want %q", got, want)
	}
}

func TestCurrentMode(t *testing.T) {
	origAudio, origSubs := *audioOnly, *subsOnly
	t.Cleanup(func() { *audioOnly, *subsOnly = origAudio, origSubs })

	tests := []struct {
		audioOnly bool
		subsOnly  bool
		want      episodeMode
	}{
		{false, false, modeFull},
		{true, false, modeAudioOnly},
		{false, true, modeSubsOnly},
	}
	for _, tc := range tests {
		*audioOnly, *subsOnly = tc.audioOnly, tc.subsOnly
		if got := currentMode(); got != tc.want {
			t.Fatalf("currentMode() with audioOnly=%v subsOnly=%v = %d, want %d",
				tc.audioOnly, tc.subsOnly, got, tc.want)
		}
	}
}

func TestSidecarDir(t *testing.T) {
	series := t.TempDir()
	dir, err := sidecarDir(series, "Season 01", "en-US")
	if err != nil {
		t.Fatalf("sidecarDir() = %v", err)
	}
	want := filepath.Join(series, "Season 01", "en-US")
	if dir != want {
		t.Fatalf("sidecarDir() = %q, want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("sidecarDir() did not create the directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("sidecarDir() created %q, which is not a directory", dir)
	}
}
