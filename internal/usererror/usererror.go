// Package usererror turns low-level errors into actionable user-facing messages.
package usererror

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
)

// Message returns a concise explanation for common failure classes while keeping
// the original error details available for debugging.
func Message(err error) string {
	if err == nil {
		return ""
	}
	details := strings.TrimSpace(err.Error())
	lower := strings.ToLower(details)

	switch {
	case containsAny(lower, "http 401", "http 403"):
		return withDetails("The translation API rejected the request. Check the API key and endpoint, then try again.", details)
	case containsAny(lower, "connection refused", "no such host", "i/o timeout", "client.timeout", "deadline exceeded"):
		return withDetails("The translation API could not be reached. Make sure the server is running and the API base URL is correct.", details)
	case missingCommand(err, lower):
		return withDetails(missingCommandMessage(err), details)
	default:
		return details
	}
}

func withDetails(summary, details string) string {
	if details == "" {
		return summary
	}
	return summary + "\n\nDetails:\n" + details
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func missingCommand(err error, lower string) bool {
	var commandErr *executil.CommandError
	if errors.As(err, &commandErr) && errors.Is(commandErr.Err, exec.ErrNotFound) {
		return true
	}
	return containsAny(lower, "executable file not found", "required executable")
}

func missingCommandMessage(err error) string {
	var commandErr *executil.CommandError
	if errors.As(err, &commandErr) && strings.TrimSpace(commandErr.Name) != "" {
		return fmt.Sprintf("Required tool %q could not be started. Install it or add it to PATH, then try again.", commandErr.Name)
	}
	return "A required local tool could not be started. Install Python, FFmpeg, and FFprobe or add them to PATH, then try again."
}
