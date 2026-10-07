package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeasonFolderName(t *testing.T) {
	cases := map[int]string{
		0:   "S00",
		1:   "S01",
		12:  "S12",
		100: "S100",
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

func TestFlatSidecarLayout(t *testing.T) {
	tests := []struct {
		langs []string
		want  bool
	}{
		{[]string{"all"}, false},
		{[]string{"th-TH"}, true},
		{[]string{"th-TH", "en-US"}, true},
		{nil, true},
	}
	for _, tc := range tests {
		if got := flatSidecarLayout(tc.langs); got != tc.want {
			t.Fatalf("flatSidecarLayout(%q) = %v, want %v", tc.langs, got, tc.want)
		}
	}
}

func TestSidecarTarget(t *testing.T) {
	series := t.TempDir()
	season := "S01"

	// Folder layout ("all"): <series>/<season>/<locale>/<base>.<ext>
	path, err := sidecarTarget(series, season, "th-TH", "S02E01 - Foo", "ass", false, false, false)
	if err != nil {
		t.Fatalf("sidecarTarget() = %v", err)
	}
	if want := filepath.Join(series, season, "th-TH", "S02E01 - Foo.ass"); path != want {
		t.Fatalf("sidecarTarget() folder = %q, want %q", path, want)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
		t.Fatalf("sidecarTarget() did not create the locale folder: %v", err)
	}

	// Flat, single language: no locale in the name.
	path, err = sidecarTarget(series, season, "th-TH", "S02E01 - Foo", "ass", false, true, false)
	if err != nil {
		t.Fatalf("sidecarTarget() = %v", err)
	}
	if want := filepath.Join(series, "S02E01 - Foo.ass"); path != want {
		t.Fatalf("sidecarTarget() flat single = %q, want %q", path, want)
	}

	// Flat, several languages: locale becomes part of the name.
	path, err = sidecarTarget(series, season, "en-US", "S02E01 - Foo", "vtt", true, true, true)
	if err != nil {
		t.Fatalf("sidecarTarget() = %v", err)
	}
	if want := filepath.Join(series, "S02E01 - Foo.en-US [CC].vtt"); path != want {
		t.Fatalf("sidecarTarget() flat multi = %q, want %q", path, want)
	}

	// Both flat writes exist without clobbering each other.
	if path == filepath.Join(series, "S02E01 - Foo [CC].vtt") {
		t.Fatalf("flat multi name unexpectedly matches the single-language name")
	}
}
