// Package srt parses and writes subtitle files used by the dubbing pipeline.
package srt

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/atomicfile"
)

var (
	blankLines    = regexp.MustCompile(`\n\s*\n`)
	timestampLine = regexp.MustCompile(`^\s*(\d{2}:\d{2}:\d{2}[,.]\d{3})\s*-->\s*(\d{2}:\d{2}:\d{2}[,.]\d{3})(?:\s+.*)?$`)
	segmentLine   = regexp.MustCompile(`^\s*(\d{2}:\d{2}:\d{2}[,.]\d{3})\s*-->\s*(\d{2}:\d{2}:\d{2}[,.]\d{3})(?:\t+| {2,})(.*)$`)
	whitespace    = regexp.MustCompile(`\s+`)
)

// Cue is one timed subtitle entry.
type Cue struct {
	Index int
	Start time.Duration
	End   time.Duration
	Text  string
}

// Duration returns the cue's positive time span.
func (c Cue) Duration() time.Duration { return c.End - c.Start }

// Parse parses UTF-8 SRT content. It tolerates missing numeric indices and
// optional positioning metadata after the end timestamp.
func Parse(content string) ([]Cue, error) {
	content = normalizeNewlines(content)
	blocks := blankLines.Split(strings.TrimSpace(content), -1)
	cues := make([]Cue, 0, len(blocks))
	nextIndex := 1

	for _, block := range blocks {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) < 2 {
			continue
		}

		index := nextIndex
		timestampPos := 0
		if parsed, err := strconv.Atoi(strings.TrimSpace(lines[0])); err == nil {
			index = parsed
			timestampPos = 1
		}
		if timestampPos >= len(lines) {
			continue
		}

		matches := timestampLine.FindStringSubmatch(strings.TrimSpace(lines[timestampPos]))
		if matches == nil {
			continue
		}
		start, err := ParseTimestamp(matches[1])
		if err != nil {
			return nil, fmt.Errorf("cue %d start timestamp: %w", index, err)
		}
		end, err := ParseTimestamp(matches[2])
		if err != nil {
			return nil, fmt.Errorf("cue %d end timestamp: %w", index, err)
		}
		if timestampPos+1 >= len(lines) {
			continue
		}
		text := strings.TrimSpace(strings.Join(lines[timestampPos+1:], "\n"))
		if text == "" || end <= start {
			continue
		}
		cues = append(cues, Cue{Index: index, Start: start, End: end, Text: text})
		nextIndex++
	}

	return sortAndValidate(cues)
}

// ParseSegments parses the tab- or multi-space-separated timestamp format
// emitted by the Whisper transcription stage:
//
//	HH:MM:SS,mmm --> HH:MM:SS,mmm<TAB>text
func ParseSegments(content string) ([]Cue, error) {
	content = normalizeNewlines(content)
	cues := make([]Cue, 0)
	for lineNumber, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		matches := segmentLine.FindStringSubmatch(rawLine)
		if matches == nil {
			return nil, fmt.Errorf("line %d is not in segments format", lineNumber+1)
		}
		start, err := ParseTimestamp(matches[1])
		if err != nil {
			return nil, fmt.Errorf("line %d start timestamp: %w", lineNumber+1, err)
		}
		end, err := ParseTimestamp(matches[2])
		if err != nil {
			return nil, fmt.Errorf("line %d end timestamp: %w", lineNumber+1, err)
		}
		text := strings.TrimSpace(matches[3])
		if text == "" || end <= start {
			continue
		}
		cues = append(cues, Cue{Index: len(cues) + 1, Start: start, End: end, Text: text})
	}
	return sortAndValidate(cues)
}

// ReadFile reads and parses an SRT file.
func ReadFile(path string) ([]Cue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SRT %q: %w", path, err)
	}
	cues, err := Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse SRT %q: %w", path, err)
	}
	return cues, nil
}

// ReadTimestampedFile accepts either .srt or the project's .segments.txt
// format. Unknown extensions are tried as segments first, then SRT.
func ReadTimestampedFile(path string) ([]Cue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read timestamped text %q: %w", path, err)
	}
	content := string(data)
	if strings.EqualFold(filepath.Ext(path), ".srt") {
		cues, parseErr := Parse(content)
		if parseErr != nil {
			return nil, fmt.Errorf("parse SRT %q: %w", path, parseErr)
		}
		return cues, nil
	}
	segments, segmentsErr := ParseSegments(content)
	if segmentsErr == nil {
		return segments, nil
	}
	cues, srtErr := Parse(content)
	if srtErr == nil {
		return cues, nil
	}
	return nil, fmt.Errorf("parse %q as segments (%v) or SRT (%v)", path, segmentsErr, srtErr)
}

// Build serializes cues as standard SRT.
func Build(cues []Cue) string {
	var builder strings.Builder
	for i, cue := range cues {
		index := cue.Index
		if index <= 0 {
			index = i + 1
		}
		fmt.Fprintf(&builder, "%d\n%s --> %s\n%s\n\n", index, FormatTimestamp(cue.Start), FormatTimestamp(cue.End), strings.TrimSpace(cue.Text))
	}
	return builder.String()
}

// WriteFile writes an SRT file atomically where the operating system permits.
func WriteFile(path string, cues []Cue) error {
	return atomicWrite(path, []byte(Build(cues)), 0o644)
}

// WriteSegmentsFile writes the reference project's timestamped text format.
func WriteSegmentsFile(path string, cues []Cue) error {
	var builder strings.Builder
	for _, cue := range cues {
		text := whitespace.ReplaceAllString(strings.ReplaceAll(cue.Text, "\n", " "), " ")
		fmt.Fprintf(&builder, "%s --> %s\t%s\n", FormatTimestamp(cue.Start), FormatTimestamp(cue.End), strings.TrimSpace(text))
	}
	return atomicWrite(path, []byte(builder.String()), 0o644)
}

// WritePlainText writes a transcript without timestamps.
func WritePlainText(path string, cues []Cue) error {
	writer := &strings.Builder{}
	buf := bufio.NewWriter(writer)
	for i, cue := range cues {
		if i > 0 {
			_ = buf.WriteByte(' ')
		}
		_, _ = buf.WriteString(whitespace.ReplaceAllString(cue.Text, " "))
	}
	_ = buf.WriteByte('\n')
	_ = buf.Flush()
	return atomicWrite(path, []byte(writer.String()), 0o644)
}

// ParseTimestamp parses HH:MM:SS,mmm or HH:MM:SS.mmm.
func ParseTimestamp(value string) (time.Duration, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), ",", ".")
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil || hours < 0 {
		return 0, fmt.Errorf("invalid hours in %q", value)
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil || minutes < 0 || minutes > 59 {
		return 0, fmt.Errorf("invalid minutes in %q", value)
	}
	secondsParts := strings.Split(parts[2], ".")
	if len(secondsParts) != 2 || len(secondsParts[1]) != 3 {
		return 0, fmt.Errorf("invalid seconds in %q", value)
	}
	seconds, err := strconv.Atoi(secondsParts[0])
	if err != nil || seconds < 0 || seconds > 59 {
		return 0, fmt.Errorf("invalid seconds in %q", value)
	}
	milliseconds, err := strconv.Atoi(secondsParts[1])
	if err != nil || milliseconds < 0 || milliseconds > 999 {
		return 0, fmt.Errorf("invalid milliseconds in %q", value)
	}
	return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second + time.Duration(milliseconds)*time.Millisecond, nil
}

// FormatTimestamp formats a duration as HH:MM:SS,mmm.
func FormatTimestamp(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	milliseconds := value.Round(time.Millisecond).Milliseconds()
	hours := milliseconds / 3_600_000
	milliseconds %= 3_600_000
	minutes := milliseconds / 60_000
	milliseconds %= 60_000
	seconds := milliseconds / 1_000
	milliseconds %= 1_000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, milliseconds)
}

func sortAndValidate(cues []Cue) ([]Cue, error) {
	if len(cues) == 0 {
		return nil, fmt.Errorf("no valid subtitle cues found")
	}
	sort.SliceStable(cues, func(i, j int) bool {
		if cues[i].Start == cues[j].Start {
			if cues[i].End == cues[j].End {
				return cues[i].Index < cues[j].Index
			}
			return cues[i].End < cues[j].End
		}
		return cues[i].Start < cues[j].Start
	})
	return cues, nil
}

func normalizeNewlines(content string) string {
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.ReplaceAll(content, "\r", "\n")
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %q: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, ".atomic-write-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := atomicfile.Replace(tempName, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}
