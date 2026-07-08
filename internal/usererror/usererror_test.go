package usererror

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
)

func TestMessageExplainsHTTPAuthFailures(t *testing.T) {
	err := fmt.Errorf("Translate subtitles: %w", errors.New("API request failed with HTTP 401: bad key"))

	got := Message(err)

	if !strings.Contains(got, "translation API rejected") || !strings.Contains(got, "Details:") || !strings.Contains(got, "HTTP 401") {
		t.Fatalf("message = %q, want auth guidance plus details", got)
	}
}

func TestMessageExplainsConnectivityFailures(t *testing.T) {
	err := errors.New(`Get "http://localhost:8000/v1/models": dial tcp 127.0.0.1:8000: connect: connection refused`)

	got := Message(err)

	if !strings.Contains(got, "could not be reached") || !strings.Contains(got, "server is running") {
		t.Fatalf("message = %q, want connectivity guidance", got)
	}
}

func TestMessageExplainsMissingCommand(t *testing.T) {
	err := &executil.CommandError{Name: "ffmpeg", Err: exec.ErrNotFound}

	got := Message(err)

	if !strings.Contains(got, `Required tool "ffmpeg" could not be started`) || !strings.Contains(got, "PATH") {
		t.Fatalf("message = %q, want missing tool guidance", got)
	}
}

func TestMessageFallsBackToRawError(t *testing.T) {
	err := errors.New("unexpected low-level failure")

	got := Message(err)

	if got != err.Error() {
		t.Fatalf("message = %q, want raw error", got)
	}
}
