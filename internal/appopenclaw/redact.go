package appopenclaw

import (
	"regexp"
	"strings"
)

// sensitiveLabels are the credential-bearing names matched in "key=value", JSON and
// flag forms. Plural "tokens" never matches because every pattern requires a
// separator directly after the name, so token counts stay readable.
const sensitiveLabels = `(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret)`

// secretFlag matches "--token value" style CLI flags, including prefixed names
// such as --gateway-token. The value may not start with "-", so a following
// flag is never consumed.
var secretFlag = regexp.MustCompile(`(?i)(--(?:[a-z0-9]+-)*` + sensitiveLabels + `\s+)(?:"[^"]*"|'[^']*'|[^\s"'-]\S*)`)

// urlUserinfo matches the userinfo part of a URL (scheme://user:pw@host).
var urlUserinfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^\s/@?#]+@`)

var bearerSecret = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)

// authorizationSecret keeps an optional scheme word (Basic, Bearer, ...) and
// redacts the credential that follows it.
var authorizationSecret = regexp.MustCompile(`(?i)(\bauthorization\\?["']?\s*[:=]\s*\\?["']?)([A-Za-z]+\s+)?[^\s,"'}\\]+`)

// labeledSecret matches key/value pairs in plain, JSON, and escaped-JSON
// (\"key\":\"value\") forms. Quoted values are redacted up to their closing
// quote so values containing spaces do not leak their tail. Capture groups
// preserve the surrounding quotes; see labeledReplacement.
var labeledSecret = regexp.MustCompile(`(?i)(` + sensitiveLabels + `\\?["']?\s*[:=]\s*)` +
	`(?:(\\")(?:.*?)(\\")` + // escaped JSON string
	`|(")(?:[^"\\]|\\.)*(")` + // JSON / double-quoted string
	`|(')[^']*(')` + // single-quoted string
	`|(\\?["']?)[^\s,"'}\\]+)`) // bare or unterminated value

const labeledReplacement = `${1}${2}${4}${6}${8}[REDACTED]${3}${5}${7}`

// knownSecret matches self-identifying credential formats: OpenAI-style sk-,
// GitHub (ghp_/gho_/ghu_/ghs_/ghr_), Slack (xoxa/b/p/r/s-), Google (AIza) and
// Telegram bot tokens (optionally prefixed by "bot" as in Bot API URLs).
var knownSecret = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{8,}` +
	`|gh[pousr]_[A-Za-z0-9]{20,}` +
	`|xox[abprs]-[A-Za-z0-9-]{10,}` +
	`|AIza[0-9A-Za-z_-]{35}` +
	`|(?:bot)?\d{6,}:[A-Za-z0-9_-]{30,})`)

// Redact removes common credential formats from operational text. Arbitrary
// transcript/secret-store payloads are never collected as log substitutes.
func Redact(text string) string {
	if !mayContainSecret(text) {
		return text
	}
	return redactPatterns(text)
}

// redactTriggers are ASCII literals, one of which every redaction pattern
// needs to match (case-insensitively) somewhere in the text.
var redactTriggers = []string{
	"token", "password", "secret", "key", "bearer", "authorization",
	"sk-", "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "xox", "aiza",
}

// mayContainSecret is an exact prefilter for redactPatterns: it returns false
// only when no pattern can match, so most log lines skip the regex passes.
// Non-ASCII text always takes the full path because (?i) folds some
// non-ASCII runes onto ASCII letters (e.g. U+212A KELVIN SIGN onto "k").
func mayContainSecret(text string) bool {
	for i := range len(text) {
		if text[i] >= 0x80 {
			return true
		}
	}
	lower := strings.ToLower(text)
	for _, lit := range redactTriggers {
		if strings.Contains(lower, lit) {
			return true
		}
	}
	if strings.Contains(text, "://") && strings.Contains(text, "@") {
		return true // urlUserinfo
	}
	return hasTelegramTokenTail(text)
}

// hasTelegramTokenTail reports whether a ':' is followed by at least 30
// token characters, which knownSecret's Telegram alternative requires.
func hasTelegramTokenTail(text string) bool {
	run := -1 // -1: not counting since the last ':'
	for i := range len(text) {
		c := text[i]
		switch {
		case c == ':':
			run = 0
		case run >= 0 && (c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'):
			run++
			if run >= 30 {
				return true
			}
		default:
			run = -1
		}
	}
	return false
}

// redactPatterns applies every redaction pattern.
func redactPatterns(text string) string {
	text = secretFlag.ReplaceAllString(text, "${1}[REDACTED]")
	text = urlUserinfo.ReplaceAllString(text, "${1}[REDACTED]@")
	text = bearerSecret.ReplaceAllString(text, "Bearer [REDACTED]")
	text = authorizationSecret.ReplaceAllString(text, "${1}${2}[REDACTED]")
	text = labeledSecret.ReplaceAllString(text, labeledReplacement)
	return knownSecret.ReplaceAllString(text, "[REDACTED]")
}
