package gui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAppendDisplayLogKeepsNewestCompleteLines(t *testing.T) {
	var builder strings.Builder

	appendDisplayLog(&builder, "alpha", 12)
	appendDisplayLog(&builder, "bravo", 12)
	text := appendDisplayLog(&builder, "charlie", 12)

	if text != "charlie" {
		t.Fatalf("text = %q, want %q", text, "charlie")
	}
	if builder.String() != text {
		t.Fatalf("builder = %q, want %q", builder.String(), text)
	}
}

func TestAppendDisplayLogPreservesValidUTF8(t *testing.T) {
	var builder strings.Builder

	appendDisplayLog(&builder, "\u00e1\u00e1\u00e1\u00e1\u00e1", 6)
	text := appendDisplayLog(&builder, "fim", 6)

	if !utf8.ValidString(text) {
		t.Fatalf("text is not valid UTF-8: %q", text)
	}
	if text != "fim" {
		t.Fatalf("text = %q, want %q", text, "fim")
	}
}

func TestCursorEnd(t *testing.T) {
	row, column := cursorEnd("one\nd\u00f3i")

	if row != 1 || column != 3 {
		t.Fatalf("cursor = (%d, %d), want (1, 3)", row, column)
	}
}
