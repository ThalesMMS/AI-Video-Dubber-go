package executil

import "regexp"

const redactedSecret = "[REDACTED]"

var (
	authorizationBearerPattern = regexp.MustCompile(`(?i)\b(authorization\s*:\s*bearer)\s+([A-Za-z0-9._~+/=-]+)`)
	bearerPattern              = regexp.MustCompile(`(?i)\b(bearer)\s+([A-Za-z0-9._~+/=-]{8,})`)
	secretAssignmentPattern    = regexp.MustCompile(`(?i)\b([A-Z0-9_.-]*(?:api[-_]?key|access[-_]?token|refresh[-_]?token|auth[-_]?token|token|secret|password|passwd|pwd))\b(\s*[=:]\s*)([^\s'"<>]+)`)
	secretFlagPattern          = regexp.MustCompile(`(?i)(--(?:api[-_]?key|access[-_]?token|refresh[-_]?token|auth[-_]?token|token|secret|password|passwd|pwd))(\s+|=)([^\s'"<>]+)`)
)

// RedactSecrets removes common credential shapes before text reaches logs or errors.
func RedactSecrets(text string) string {
	if text == "" {
		return text
	}
	text = authorizationBearerPattern.ReplaceAllString(text, `${1} `+redactedSecret)
	text = secretFlagPattern.ReplaceAllString(text, `${1}${2}`+redactedSecret)
	text = secretAssignmentPattern.ReplaceAllString(text, `${1}${2}`+redactedSecret)
	text = bearerPattern.ReplaceAllString(text, `${1} `+redactedSecret)
	return text
}
