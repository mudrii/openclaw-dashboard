package appopenclaw

import "regexp"

var bearerSecret = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
var labeledSecret = regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret|authorization)["']?\s*[:=]\s*["']?)[^\s,"'}]+`)
var apiSecret = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`)

// Redact removes common credential formats from operational text. Arbitrary
// transcript/secret-store payloads are never collected as log substitutes.
func Redact(text string) string {
	text = bearerSecret.ReplaceAllString(text, "Bearer [REDACTED]")
	text = labeledSecret.ReplaceAllString(text, "${1}[REDACTED]")
	return apiSecret.ReplaceAllString(text, "[REDACTED]")
}
