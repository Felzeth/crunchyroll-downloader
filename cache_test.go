package main

import (
	"os"
	"path/filepath"
	"testing"
)

// withCache points the cache at a temporary directory and toggles -cache for
// the duration of the test.
func withCache(t *testing.T, enabled bool) string {
	t.Helper()
	oldEnabled := *enableCache
	oldDir := cacheDirOverride
	*enableCache = enabled
	cacheDirOverride = t.TempDir()
	t.Cleanup(func() {
		*enableCache = oldEnabled
		cacheDirOverride = oldDir
	})
	return cacheDirOverride
}

func TestCachedTrackPathDisabled(t *testing.T) {
	withCache(t, false)
	if path, ok := cachedTrackPath("audio", "G123", "192k"); ok || path != "" {
		t.Fatalf("cachedTrackPath() = (%q, %v), want (\"\", false)", path, ok)
	}
}

func TestCachedTrackPathEnabled(t *testing.T) {
	dir := withCache(t, true)

	path, ok := cachedTrackPath("video", "G123", "1080p")
	if !ok {
		t.Fatal("cachedTrackPath() ok = false, want true")
	}
	if got, want := filepath.Dir(path), filepath.Join(dir, cacheDirName); got != want {
		t.Fatalf("cachedTrackPath() dir = %q, want %q", got, want)
	}
	if got, want := filepath.Base(path), "video_G123_1080p.mp4"; got != want {
		t.Fatalf("cachedTrackPath() base = %q, want %q", got, want)
	}
	if _, ok := cachedTrackPath("audio", "", "192k"); ok {
		t.Fatal("cachedTrackPath() with an empty content ID reported ok, want false")
	}
}

func TestStoreAndLookupCachedTrack(t *testing.T) {
	withCache(t, true)

	if got := lookupCachedTrack("audio", "G1", "192k"); got != "" {
		t.Fatalf("lookupCachedTrack() on an empty cache = %q, want \"\"", got)
	}

	src := filepath.Join(t.TempDir(), "track")
	if err := os.WriteFile(src, []byte("decrypted bytes"), 0666); err != nil {
		t.Fatal(err)
	}
	storeCachedTrack("audio", "G1", "192k", src)

	got := lookupCachedTrack("audio", "G1", "192k")
	if got == "" {
		t.Fatal("lookupCachedTrack() after store = \"\", want a copy")
	}
	defer os.Remove(got)

	if got == src {
		t.Fatal("lookupCachedTrack() returned the source path instead of a copy")
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "decrypted bytes" {
		t.Fatalf("cached copy = %q, want %q", data, "decrypted bytes")
	}
}

func TestStoreCachedTrackKeepsExistingEntry(t *testing.T) {
	withCache(t, true)

	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	if err := os.WriteFile(first, []byte("first"), 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0666); err != nil {
		t.Fatal(err)
	}

	storeCachedTrack("video", "G1", "1080p", first)
	storeCachedTrack("video", "G1", "1080p", second) // must not replace the first

	got := lookupCachedTrack("video", "G1", "1080p")
	if got == "" {
		t.Fatal("lookupCachedTrack() after store = \"\", want a hit")
	}
	defer os.Remove(got)
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first" {
		t.Fatalf("cache entry = %q, want the first stored value", data)
	}
}

func TestLookupCachedTrackIgnoresEmptyFile(t *testing.T) {
	withCache(t, true)

	path, _ := cachedTrackPath("audio", "G1", "192k")
	if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0666); err != nil {
		t.Fatal(err)
	}

	if got := lookupCachedTrack("audio", "G1", "192k"); got != "" {
		t.Fatalf("lookupCachedTrack() on an empty cache file = %q, want \"\"", got)
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	full := filepath.Join(dir, "full")
	if err := os.WriteFile(empty, nil, 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("x"), 0666); err != nil {
		t.Fatal(err)
	}

	if fileExists(empty) {
		t.Fatal("fileExists(empty) = true, want false")
	}
	if !fileExists(full) {
		t.Fatal("fileExists(full) = false, want true")
	}
	if fileExists(filepath.Join(dir, "missing")) {
		t.Fatal("fileExists(missing) = true, want false")
	}
	if fileExists(dir) {
		t.Fatal("fileExists(dir) = true, want false")
	}
}

func TestMoveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "sub", "dst")
	if err := os.WriteFile(src, []byte("payload"), 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0777); err != nil {
		t.Fatal(err)
	}

	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile() = %v, want nil", err)
	}
	if fileExists(src) {
		t.Fatal("moveFile() left the source behind")
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("moved file = %q, want %q", data, "payload")
	}
	if err := moveFile(src, dst); err == nil {
		t.Fatal("moveFile() from a missing source = nil, want an error")
	}
}
