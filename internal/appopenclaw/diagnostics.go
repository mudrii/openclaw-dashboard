package appopenclaw

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"
)

// MaxDiagnosticBytes bounds the redacted command output kept for a single
// server-side log line. Browser payloads never carry this text.
const MaxDiagnosticBytes = 512

// redactedTail returns the log-safe tail of raw command output: at most
// MaxDiagnosticBytes of Redact-ed text with newlines collapsed into spaces.
// Redaction runs before truncation on purpose — trimming first could cut the
// leading marker off a credential and let the remainder escape the patterns.
func redactedTail(output []byte) string {
	text := Redact(string(output))
	if len(text) > MaxDiagnosticBytes {
		text = text[len(text)-MaxDiagnosticBytes:]
		for len(text) > 0 && !utf8.RuneStart(text[0]) {
			text = text[1:]
		}
	}
	lines := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' })
	return strings.TrimSpace(strings.Join(lines, " "))
}

// logCollectionFailure records a failed runtime call for the operator. The
// diagnostic tail is redacted by CommandError before it is stored on the error,
// so no un-redacted byte can reach the logger from here.
func logCollectionFailure(ctx context.Context, method string, err error) {
	var collection *readError
	detail := ""
	if errors.As(err, &collection) {
		detail = collection.detail
	}
	slog.WarnContext(ctx, "[openclaw] collection failed",
		"method", method,
		"code", ErrorCode(err),
		"target", TargetFromContext(ctx).Effective(),
		"stderr", detail,
	)
}
