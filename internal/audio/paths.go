package audio

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
)

// Paths contains the deterministic intermediate/output files used by a run.
type Paths struct {
	Input          string
	Base           string
	ExtractedAudio string
	TranscriptSRT  string
	SegmentsTXT    string
	TranscriptJSON string
	TranscriptTXT  string
	TranslatedSRT  string
	SyncedAudio    string
	FinalVideo     string
}

// BuildPaths creates paths next to the input video, matching the Python project.
func BuildPaths(inputPath, languageCode, explicitOutput string) (Paths, error) {
	return BuildPathsForMode(inputPath, languageCode, explicitOutput, config.ModeDub)
}

// BuildPathsForMode creates deterministic paths for the selected complete run mode.
func BuildPathsForMode(inputPath, languageCode, explicitOutput string, mode config.Mode) (Paths, error) {
	return BuildPathsForModeOptions(inputPath, languageCode, explicitOutput, mode, false)
}

// BuildPathsForModeOptions creates deterministic paths for the selected complete run mode and output style.
func BuildPathsForModeOptions(inputPath, languageCode, explicitOutput string, mode config.Mode, subtitleBurnIn bool) (Paths, error) {
	absolute, err := filepath.Abs(inputPath)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve input path: %w", err)
	}
	extension := filepath.Ext(absolute)
	if extension == "" {
		return Paths{}, fmt.Errorf("input file has no extension: %s", absolute)
	}
	base := strings.TrimSuffix(absolute, extension)
	finalSuffix := "synced"
	parsedMode, err := config.ParseMode(string(mode))
	if err != nil {
		return Paths{}, err
	}
	if parsedMode == config.ModeSubtitle {
		finalSuffix = "subtitled"
		if subtitleBurnIn {
			finalSuffix = "burned-in"
		}
	}
	finalVideo := strings.TrimSpace(explicitOutput)
	if finalVideo == "" {
		finalVideo = fmt.Sprintf("%s.%s.%s.mp4", base, languageCode, finalSuffix)
	} else if !filepath.IsAbs(finalVideo) {
		finalVideo, err = filepath.Abs(finalVideo)
		if err != nil {
			return Paths{}, fmt.Errorf("resolve output path: %w", err)
		}
	}
	return Paths{
		Input:          absolute,
		Base:           base,
		ExtractedAudio: base + ".mp3",
		TranscriptSRT:  base + ".srt",
		SegmentsTXT:    base + ".segments.txt",
		TranscriptJSON: base + ".json",
		TranscriptTXT:  base + ".txt",
		TranslatedSRT:  fmt.Sprintf("%s.%s.srt", base, languageCode),
		SyncedAudio:    fmt.Sprintf("%s.%s.synced.mp3", base, languageCode),
		FinalVideo:     finalVideo,
	}, nil
}
