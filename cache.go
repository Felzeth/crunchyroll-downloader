package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// cacheDirName is the subdirectory of the OS cache directory that -cache uses
// to store decrypted tracks between runs.
const cacheDirName = "crunchyroll-downloader"

// cacheDirOverride, when non-empty, replaces the OS cache directory. Tests set
// it to a temporary directory so they never touch the real cache.
var cacheDirOverride string

// cacheBaseDir returns the directory holding cached tracks. It falls back to
// the working directory when the OS cache directory can't be resolved.
func cacheBaseDir() string {
	base := cacheDirOverride
	if base == "" {
		if dir, err := os.UserCacheDir(); err == nil {
			base = dir
		}
	}
	if base == "" {
		base = "."
	}
	return filepath.Join(base, cacheDirName)
}

// cachedTrackPath returns where a decrypted track would be cached, and whether
// caching is enabled. The key combines the content ID (unique per episode and
// per dub) with the requested quality, so different qualities never collide.
// Both audio and video are fragmented MP4 once decrypted, hence the ".mp4".
func cachedTrackPath(kind, contentId, quality string) (string, bool) {
	if !*enableCache || contentId == "" {
		return "", false
	}
	name := fmt.Sprintf("%s_%s_%s.mp4", kind, contentId, quality)
	return filepath.Join(cacheBaseDir(), name), true
}

// lookupCachedTrack returns a temporary copy of a cached decrypted track, or ""
// when there is no usable entry. Callers own the returned file and must delete
// it, exactly like a freshly downloaded track.
func lookupCachedTrack(kind, contentId, quality string) string {
	path, ok := cachedTrackPath(kind, contentId, quality)
	if !ok {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return ""
	}

	tmp, err := tempTrackFile(kind)
	if err != nil {
		return ""
	}
	if err := copyFile(path, tmp); err != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return tmp
}

// storeCachedTrack copies a freshly downloaded track into the cache. Caching is
// best-effort: any failure is ignored so a download never fails because of it.
func storeCachedTrack(kind, contentId, quality, path string) {
	cachePath, ok := cachedTrackPath(kind, contentId, quality)
	if !ok {
		return
	}
	if _, err := os.Stat(cachePath); err == nil {
		return // already cached
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0777); err != nil {
		return
	}
	// Copy to a unique temporary file first so an interrupted write can't leave
	// a truncated entry that a later run would happily reuse.
	tmp := cachePath + ".tmp"
	if err := copyFile(path, tmp); err != nil {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		_ = os.Remove(tmp)
	}
}

// tempTrackFile creates an empty temporary file for a decrypted track.
func tempTrackFile(kind string) (string, error) {
	f, err := os.CreateTemp("", "crdl-"+kind+"-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	f.Close()
	return name, nil
}

// copyFile copies src to dst, replacing dst if it already exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

// moveFile relocates src to dst, falling back to a copy when the two live on
// different filesystems (rename across devices fails). src is gone either way.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	_ = os.Remove(src)
	return nil
}

// fileExists reports whether path is an existing non-empty regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}
