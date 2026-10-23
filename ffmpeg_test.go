package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFindFFmpegInFindsAbsolutePath covers the regression behind
// "exec: \"ffmpeg\": cannot run executable found relative to current directory":
// the resolved path must always be absolute, even when the executable is found
// in a directory that happens to be the working directory.
func TestFindFFmpegInFindsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, ffmpegNames()[0])
	if err := os.WriteFile(exe, []byte("stub"), 0o666); err != nil {
		t.Fatal(err)
	}

	got, err := findFFmpegIn([]string{dir})
	if err != nil {
		t.Fatalf("findFFmpegIn returned error: %v", err)
	}
	if got != exe {
		t.Errorf("findFFmpegIn = %q, want %q", got, exe)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("findFFmpegIn returned a relative path %q", got)
	}
}

func TestFindFFmpegInSkipsDirectoriesAndEmptyEntries(t *testing.T) {
	dir := t.TempDir()
	// A directory that merely shares the executable's name must not count.
	if err := os.Mkdir(filepath.Join(dir, ffmpegNames()[0]), 0o777); err != nil {
		t.Fatal(err)
	}

	if got, err := findFFmpegIn([]string{"", dir}); err == nil {
		t.Fatalf("findFFmpegIn = %q, want an error", got)
	}
}

func TestFindFFmpegInMissing(t *testing.T) {
	if got, err := findFFmpegIn([]string{t.TempDir()}); err == nil {
		t.Fatalf("findFFmpegIn = %q, want an error", got)
	}
}

func TestFFmpegSearchDirsUsesAbsolutePathEntries(t *testing.T) {
	abs := t.TempDir()
	sep := string(filepath.ListSeparator)
	t.Setenv("PATH", "."+sep+abs)

	dirs := ffmpegSearchDirs()
	found := false
	for _, dir := range dirs {
		if !filepath.IsAbs(dir) {
			t.Errorf("search dir %q is not absolute", dir)
		}
		if filepath.Clean(dir) == filepath.Clean(abs) {
			found = true
		}
	}
	if !found {
		t.Errorf("search dirs %v do not include absolute PATH entry %q", dirs, abs)
	}
}

// TestFindFFmpegFindsProcDirectory checks the end-to-end helper against the
// directory that holds the test binary, which is always absolute.
func TestFindFFmpegFindsProcDirectory(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable unavailable: %v", err)
	}
	dir := filepath.Dir(exe)

	got, err := findFFmpegIn([]string{dir})
	if err != nil {
		t.Skipf("no ffmpeg next to the test binary: %v", err)
	}
	if !filepath.IsAbs(got) || strings.ToLower(filepath.Dir(got)) != strings.ToLower(dir) {
		t.Errorf("findFFmpegIn(%q) = %q, want an absolute path inside %q", dir, got, dir)
	}
}
