package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEpisodeSubBaseName(t *testing.T) {
	info := EpisodeInfo{
		Title: "The Man Who Became a Kaiju",
		EpisodeMetadata: EpisodeMetadata{
			// The colon is illegal in Windows file names and is sanitized.
			SeriesTitle:   "Kaiju No. 8",
			SeasonNumber:  1,
			EpisodeNumber: 1,
		},
	}
	got, want := episodeSubBaseName(info), "Kaiju No. 8 S01E01 - The Man Who Became a Kaiju"
	if got != want {
		t.Fatalf("episodeSubBaseName() = %q, want %q", got, want)
	}
}

func TestSubtitleExt(t *testing.T) {
	if got, want := subtitleExt("ass"), "ass"; got != want {
		t.Fatalf("subtitleExt(ass) = %q, want %q", got, want)
	}
	if got, want := subtitleExt("vtt"), "srt"; got != want {
		t.Fatalf("subtitleExt(vtt) = %q, want %q", got, want)
	}
}

func TestVttToSrt(t *testing.T) {
	const in = "\xef\xbb\xbfWEBVTT\r\n" +
		"\r\n" +
		"STYLE\r\n" +
		"::cue { color: red }\r\n" +
		"\r\n" +
		"NOTE this is a comment\r\n" +
		"spanning lines\r\n" +
		"\r\n" +
		"intro-cue\r\n" +
		"00:00:01.000 --> 00:00:02.5 line:90% align:center\r\n" +
		"<v Speaker>Hello <i>world</i></v>\r\n" +
		"\r\n" +
		"2\r\n" +
		"0:00:04.5 --> 00:01:05.25\r\n" +
		"Second cue\\hhere\r\n" +
		"and a second line\r\n"

	got := string(vttToSrt([]byte(in)))
	want := "1\n" +
		"00:00:01,000 --> 00:00:02,500\n" +
		"Hello world\n" +
		"2\n" +
		"00:00:04,500 --> 00:01:05,250\n" +
		"Second cue here\n" +
		"and a second line\n"
	if got != want {
		t.Fatalf("vttToSrt() =\n%q\nwant\n%q", got, want)
	}
}

func TestVttToSrtKeepsUnknownLinesAsText(t *testing.T) {
	// A text line that merely contains an arrow must not be mistaken for a
	// cue timing; it survives as cue text.
	const in = "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nA --> B\n"
	got := string(vttToSrt([]byte(in)))
	if !strings.Contains(got, "A --> B") {
		t.Fatalf("vttToSrt() dropped dialogue containing '-->': %q", got)
	}
	if !strings.HasPrefix(got, "1\n") {
		t.Fatalf("vttToSrt() produced unexpected structure: %q", got)
	}
}

func TestDownloadSubsOnlyNaming(t *testing.T) {
	subs := t.TempDir()
	season := "S02"
	base := "Kaiju No. 8 S02E01 - The Man Who Became a Kaiju"

	jobs := []subJob{
		{url: "u1", format: "ass", locale: "th-TH", flat: true},
		{url: "u2", format: "vtt", locale: "en-US", isCC: true, flat: true, multiLang: true},
	}
	// fetchSubtitle would hit the network, so exercise only the naming path
	// the download loop uses: sidecarTarget with the subtitle extension.
	for _, job := range jobs {
		target, err := sidecarTarget(subs, season, job.locale, base, subtitleExt(job.format), job.isCC, job.flat, job.multiLang)
		if err != nil {
			t.Fatalf("sidecarTarget() = %v", err)
		}
		if err := os.WriteFile(target, nil, 0666); err != nil {
			t.Fatalf("create %s: %v", target, err)
		}
	}
	if _, err := os.Stat(filepath.Join(subs, base+".ass")); err != nil {
		t.Fatalf("expected flat single-language .ass at %s: %v", filepath.Join(subs, base+".ass"), err)
	}
	if _, err := os.Stat(filepath.Join(subs, base+".en-US [CC].srt")); err != nil {
		t.Fatalf("expected flat multi-language .srt at %s: %v", filepath.Join(subs, base+".en-US [CC].srt"), err)
	}
}
