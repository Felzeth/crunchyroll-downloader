package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// vttTimingPattern is the shape of a WebVTT timestamp: an optional hour part,
// fixed minute/second parts and a 1-3 digit fraction.
const vttTimingPattern = `(?:\d+:)?\d{2}:\d{2}[.,]\d{1,3}`

// vttTiming matches a WebVTT cue timing line, capturing the start and end
// timestamps and any cue positioning settings ("line:0 align:center").
var vttTiming = regexp.MustCompile(
	`^\s*(` + vttTimingPattern + `)\s+-->\s+(` + vttTimingPattern + `)(?:\s+(.*))?\s*$`)

// vttTimestamp matches a single WebVTT timestamp, whose hour part is optional.
var vttTimestamp = regexp.MustCompile(`^(?:(\d+):)?(\d{2}):(\d{2})[.,](\d{1,3})$`)

// vttTag matches an inline WebVTT tag such as <c>, <v Speaker> or </i>. SRT has
// no equivalent for most of them, so they are stripped from the cue text.
var vttTag = regexp.MustCompile(`</?[^>]*>`)

// normalizeSrtTimestamp parses one WebVTT timestamp and renders it in the SRT
// form "HH:MM:SS,mmm": hours are zero-padded to two digits, the fraction is
// right-padded to exactly three digits and the decimal point becomes a comma.
func normalizeSrtTimestamp(ts string) (string, bool) {
	m := vttTimestamp.FindStringSubmatch(strings.TrimSpace(ts))
	if m == nil {
		return "", false
	}
	hours := m[1]
	if hours == "" {
		hours = "0"
	}
	h, err := strconv.Atoi(hours)
	if err != nil {
		return "", false
	}
	frac := m[4]
	for len(frac) < 3 {
		frac += "0"
	}
	return fmt.Sprintf("%02d:%s:%s,%s", h, m[2], m[3], frac), true
}

// vttToSrt converts a WebVTT document to SRT: timestamps are normalized, cue
// positioning settings and inline styling tags are dropped (SRT has no
// equivalents) and cues are renumbered sequentially. Anything the parser
// doesn't recognize as cue structure is kept as cue text, so odd-but-valid
// documents degrade instead of failing.
func vttToSrt(body []byte) []byte {
	// WebVTT files start with an optional UTF-8 BOM.
	text := strings.TrimPrefix(string(body), "\ufeff")
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}

	var out []string
	renumber := func() string {
		out = append(out, strconv.Itoa(len(out)/2+1))
		return out[len(out)-1]
	}
	appendText := func(line string) {
		line = strings.ReplaceAll(line, `\h`, " ")
		line = strings.TrimSpace(vttTag.ReplaceAllString(line, ""))
		if line != "" {
			out = append(out, line)
		}
	}
	skipBlock := func(i int) int {
		// NOTE/STYLE/REGION blocks run until the next blank line.
		for i < len(lines) && lines[i] != "" {
			i++
		}
		return i
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "WEBVTT") {
			i = skipBlock(i + 1)
			continue
		}
		if strings.HasPrefix(line, "NOTE") || strings.HasPrefix(line, "STYLE") || strings.HasPrefix(line, "REGION") {
			i = skipBlock(i)
			continue
		}
		if m := vttTiming.FindStringSubmatch(line); m != nil {
			start, startOK := normalizeSrtTimestamp(m[1])
			end, endOK := normalizeSrtTimestamp(m[2])
			if startOK && endOK {
				renumber()
				out = append(out, start+" --> "+end)
				continue
			}
		}
		// A cue identifier is any line immediately followed by a timing line.
		if i+1 < len(lines) && vttTiming.MatchString(lines[i+1]) {
			continue
		}
		appendText(line)
	}

	result := strings.Join(out, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return []byte(result)
}
