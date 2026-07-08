package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplacePreservesDestinationWhenSourceCannotBeRenamed(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Replace(filepath.Join(dir, "missing.tmp"), destination)
	if err == nil {
		t.Fatal("Replace succeeded with a missing source")
	}
	data, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "original" {
		t.Fatalf("destination = %q, want original preserved", string(data))
	}
}

func TestReplaceSwapsDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.tmp")
	destination := filepath.Join(dir, "output.txt")
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Replace(source, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("destination = %q, want new", string(data))
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source stat error = %v, want removed by rename", err)
	}
}
