# Crunchyroll Downloader

[![Tests](https://img.shields.io/github/actions/workflow/status/CuteTenshii/crunchyroll-downloader/tests.yml?branch=master&label=tests)](https://github.com/CuteTenshii/crunchyroll-downloader/actions/workflows/tests.yml)
[![Latest release](https://img.shields.io/github/v/release/CuteTenshii/crunchyroll-downloader)](https://github.com/CuteTenshii/crunchyroll-downloader/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/CuteTenshii/crunchyroll-downloader/total)](https://github.com/CuteTenshii/crunchyroll-downloader/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/CuteTenshii/crunchyroll-downloader)](go.mod)
[![License](https://img.shields.io/github/license/CuteTenshii/crunchyroll-downloader)](LICENSE.txt)

Downloads anime from Crunchyroll and outputs them in a MKV file.

You won't be banned or anything, I downloaded all Kaguya-Sama seasons to test during 30 mins and everything went fine

## Features

- Supports choosing the audio and subtitles language, including downloading multiple of each into a single file
- Supports choosing the audio and video quality
- `--all-audio-subs` grabs every available audio language together with every subtitle and closed caption in one file
- `--audio-only` and `--subs-only` save just the audio tracks or just the subtitles, split into one folder per language
- `--cache` reuses decrypted video and audio tracks between runs so nothing is downloaded twice
- Decrypts Widevine DRM (requires: a `.wvd` file or `client_id.bin` and `private_key.pem` files)
- Adds metadata (like episode name) to the MKV container
- Parallel segment downloads (10 workers) for faster downloads
- Retry with backoff on connection errors
- Batch download from a list of URLs

## Requirements

- [FFmpeg](https://www.ffmpeg.org/download.html#get-packages)
- To download Premium-only content, a Crunchyroll Premium account. No, this can't be bypassed and a free trial should be enough
- Either a `.wvd` file, or a `client_id.bin` and `private_key.pem`

## Download

Check the [Tenshii latest release](https://github.com/CuteTenshii/crunchyroll-downloader/releases/latest) and download the file that corresponds to your OS.

Check this [Fork release](https://github.com/felzeth/crunchyroll-downloader/release/latest) to have more additional features.

## Usage

- Open a Terminal/Command prompt, and go to the folder where you downloaded the binary/cloned the repo
- Run the program with the options you want:
```shell
Usage of ./crunchyroll-downloader:
  -all-audio-subs
        Download every available audio language together with every available subtitle and closed caption
  -audio-lang string
        Audio language(s), comma-separated for multiple (e.g. "ja-JP,en-US") or "all" for every available language. First is the default track (default "ja-JP")
  -audio-only
        Download audio only (no video or subtitles), one file per language in its own folder
  -audio-quality string
        Audio quality (default "192k")
  -cache
        Cache decrypted video and audio tracks so re-running an episode doesn't download them again
  -cc-lang string
        Closed caption language(s), comma-separated for multiple (e.g. "en-US") or "all" for every available caption. Downloaded in addition to --subs-lang, not instead of it
  -debug-manifest
        Log raw episode playback JSON and manifest XML
  -download-delay duration
        Minimum delay between episode downloads, to help avoid Crunchyroll's rate limiting (e.g. "30s", "2m")
  -etp-rt string
        The "etp_rt" cookie value of your account
  -file string
        Path to a text file with one URL per line
  -season int
        Season number. Not used if an episode link is entered
  -subs-lang string
        Subtitle language(s), comma-separated for multiple (e.g. "en-US,es-419") or "all" for every available subtitle. First is the default track (default "en-US")
  -subs-only
        Download subtitles only (no video or audio), one file per language in its own folder
  -url string
        URL of the episode/season to download
  -video-quality string
        Video quality (default "1080p")
```

### Flag aliases

A few flags accept a hidden alternate spelling. Aliases are never listed in `-h`, and both the single-dash and double-dash forms are accepted, with or without a value (`-sub-only`, `--sub-only`, `--sub-only=true`):

| Alias | Equivalent to |
| --- | --- |
| `-sub-only` | `-subs-only` |

Ex: to download the first season of *Hell's Paradise*:
```shell
./crunchyroll-downloader --url https://www.crunchyroll.com/series/GJ0H7Q5ZJ/hells-paradise --season 1 --etp-rt replace_this
```

To download a specific episode:
```shell
./crunchyroll-downloader --url https://www.crunchyroll.com/watch/GE00198973JAJP/dawn-and-confusion --etp-rt replace_this
```

To batch download from a file (one URL per line):
```shell
./crunchyroll-downloader --file list.txt --etp-rt replace_this --subs-lang pt-BR
```

To download multiple audio tracks and subtitles into a single file (the first of each is set as the default track). If any requested language is missing for an episode, that episode is skipped:
```shell
./crunchyroll-downloader --url https://www.crunchyroll.com/watch/GE00198973JAJP/dawn-and-confusion --etp-rt replace_this --audio-lang ja-JP,en-US --subs-lang en-US,es-419,de-DE
```

To download every available subtitle, pass `all` to `--subs-lang` (`--cc-lang all` does the same for closed captions):
```shell
./crunchyroll-downloader --url https://www.crunchyroll.com/series/GJ0H7Q5ZJ/hells-paradise --season 1 --etp-rt replace_this --subs-lang all
```

If you're getting rate-limited while downloading a season/batch, wait at least this long between each episode:
```shell
./crunchyroll-downloader --url https://www.crunchyroll.com/series/GJ0H7Q5ZJ/hells-paradise --season 1 --etp-rt replace_this --download-delay 30s
```

To download every audio language together with every subtitle and closed caption in a single file:
```shell
./crunchyroll-downloader --url https://www.crunchyroll.com/watch/GE00198973JAJP/dawn-and-confusion --etp-rt replace_this --all-audio-subs
```

To save only the audio, one file per language (each language gets its own folder, e.g. `Hell's Paradise/Season 01/ja-JP/`):
```shell
./crunchyroll-downloader --url https://www.crunchyroll.com/series/GJ0H7Q5ZJ/hells-paradise --season 1 --etp-rt replace_this --audio-only --all-audio-subs
```

To save only the subtitles, one file per language, in the same folder layout:
```shell
./crunchyroll-downloader --url https://www.crunchyroll.com/series/GJ0H7Q5ZJ/hells-paradise --season 1 --etp-rt replace_this --subs-only --all-audio-subs
```

To reuse decrypted video and audio tracks between runs (e.g. re-downloading an episode with different subtitles), add `--cache`. Cached tracks are stored in your OS cache directory (`%LocalAppData%\crunchyroll-downloader` on Windows, `~/.cache/crunchyroll-downloader` on Linux):
```shell
./crunchyroll-downloader --url https://www.crunchyroll.com/watch/GE00198973JAJP/dawn-and-confusion --etp-rt replace_this --cache
```

If Crunchyroll rate-limits an episode anyway, it's retried in place (starting at `-download-delay`, or 1 minute if unset, doubling up to 30 minutes on repeated hits) instead of moving on to the next episode and tripping the same limit again.

## Building

### Requirements

- [Go](https://go.dev/dl/)

### Guide

- Clone this repository
- Open a Terminal/Command prompt, and go to the folder where you cloned the repo
- Run `go build .`

## Help

### How do I get my `etp_rt` cookie?

- Go to https://crunchyroll.com
- Open Developer Tools
- Firefox: Go to *Storage* then *Cookies*<br />Chrome: Go to *Application* then *Cookies*
- Select the Crunchyroll domain, then copy the `etp_rt` cookie value

![](.github/screenshots/etp-rt-cookie.png)

### What is a `.wvd` file and do I really need one?

Yes, Crunchyroll uses DRM-only content. This file is used to get a Widevine license, which gives the keys to decrypt the media.

If you don't have a rooted Android device or are just lazy, search "ready to use cdms" and you'll find plenty of websites providing those files.

## License

This project is licensed under the MIT License. See [LICENSE.txt](LICENSE.txt)
