package srt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAndBuildSRT(t *testing.T) {
	content := "\ufeff7\r\n00:00:02,500 --> 00:00:03,750 align:start\r\nSecond line\r\ncontinued\r\n\r\n00:00:00.100 --> 00:00:01.200\r\nFirst line\r\n"
	cues, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(cues) != 2 {
		t.Fatalf("len(cues) = %d, want 2", len(cues))
	}
	if cues[0].Start != 100*time.Millisecond || cues[0].Text != "First line" {
		t.Fatalf("first cue = %#v", cues[0])
	}
	if cues[1].Index != 7 || cues[1].Text != "Second line\ncontinued" {
		t.Fatalf("second cue = %#v", cues[1])
	}
	built := Build(cues)
	if !strings.Contains(built, "00:00:02,500 --> 00:00:03,750") {
		t.Fatalf("Build() missing timestamp:\n%s", built)
	}
	roundTrip, err := Parse(built)
	if err != nil || len(roundTrip) != 2 {
		t.Fatalf("round trip failed: cues=%#v err=%v", roundTrip, err)
	}
}

func TestParseSegmentsAcceptsTabAndSpaces(t *testing.T) {
	content := "00:00:00,000 --> 00:00:01,000\tHello\n00:00:01.250 --> 00:00:02.500  World\n"
	cues, err := ParseSegments(content)
	if err != nil {
		t.Fatalf("ParseSegments() error = %v", err)
	}
	if len(cues) != 2 || cues[1].Text != "World" {
		t.Fatalf("cues = %#v", cues)
	}
}

func TestReadTimestampedFileAndAtomicOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.segments.txt")
	cues := []Cue{{Index: 1, Start: time.Second, End: 2 * time.Second, Text: "Olá"}}
	if err := WriteSegmentsFile(path, cues); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTimestampedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "Olá" {
		t.Fatalf("got = %#v", got)
	}
	if err := WriteSegmentsFile(path, []Cue{{Index: 1, Start: 0, End: time.Second, Text: "Novo"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Olá") || !strings.Contains(string(data), "Novo") {
		t.Fatalf("overwrite content = %q", data)
	}
}

func TestTimestampValidation(t *testing.T) {
	for _, value := range []string{"00:60:00,000", "00:00:60,000", "00:00:00,00", "x"} {
		if _, err := ParseTimestamp(value); err == nil {
			t.Errorf("ParseTimestamp(%q) unexpectedly succeeded", value)
		}
	}
	if got := FormatTimestamp(-time.Second); got != "00:00:00,000" {
		t.Fatalf("FormatTimestamp(-1s) = %q", got)
	}
}
