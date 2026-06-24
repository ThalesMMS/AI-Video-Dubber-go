package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	project := t.TempDir()
	cfg := (Config{APIBase: " http://localhost:8000/v1/ ", TranslationBatchSize: -1}).Normalize(project)
	if cfg.APIBase != "http://localhost:8000/v1" {
		t.Fatalf("APIBase = %q", cfg.APIBase)
	}
	if cfg.LanguageCode != "pt-BR" || cfg.WhisperModel == "" || cfg.TranslationBatchSize != DefaultBatchSize {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
	if cfg.VenvDir != filepath.Join(project, ".venv") {
		t.Fatalf("VenvDir = %q", cfg.VenvDir)
	}
	if !strings.Contains(VenvPython(cfg.VenvDir), ".venv") {
		t.Fatalf("VenvPython = %q", VenvPython(cfg.VenvDir))
	}
}
