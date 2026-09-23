package apprefresh

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// referenceParseLogTimestamp is ParseLogTimestamp without the
// onlyTimestampBytes pre-check; the two must agree on every input.
func referenceParseLogTimestamp(candidates ...string) (time.Time, string) {
	for _, candidate := range candidates {
		c := strings.TrimSpace(candidate)
		if c == "" {
			continue
		}
		for _, layout := range logTimestampLayouts {
			if parsed, err := time.ParseInLocation(layout, c, time.Local); err == nil {
				return parsed, c
			}
		}
	}
	for _, candidate := range candidates {
		c := strings.TrimSpace(candidate)
		if c == "" {
			continue
		}
		if match := reLogPrefix.FindStringSubmatch(c); len(match) >= 2 && match[1] != c {
			return referenceParseLogTimestamp(match[1])
		}
	}
	return time.Time{}, ""
}

func assertParseLogTimestampMatchesReference(t *testing.T, candidates ...string) {
	t.Helper()
	gotTime, gotSeen := ParseLogTimestamp(candidates...)
	wantTime, wantSeen := referenceParseLogTimestamp(candidates...)
	if gotSeen != wantSeen || !gotTime.Equal(wantTime) || gotTime.Location().String() != wantTime.Location().String() {
		t.Fatalf("ParseLogTimestamp(%q) = (%v, %q), reference = (%v, %q)", candidates, gotTime, gotSeen, wantTime, wantSeen)
	}
}

func TestParseLogTimestampMatchesReference(t *testing.T) {
	for _, c := range []string{
		"2026-09-23T10:11:12Z",
		"2026-09-23T10:11:12.123456789Z",
		"2026-09-23T10:11:12,5Z",
		"2026-09-23T10:11:12.1234567890123+08:00",
		"2026-09-23t10:11:12z",
		"2026-09-23T10:11:12z",
		"2026-09-23 10:11:12",
		"2026-09-23  10:11:12",
		"2026-09-23 10:11:12.5",
		"2026-09-23T10:11:12",
		"+2026-09-23T10:11:12",
		"2026-09-23T10:11:12-07:00",
		"2026-09-23T10:11:12 +0700",
		"2026-09-23T10:11:12 UTC",
		"2026-13-45T25:61:99",
		"2026-09-23T10:11:12Z [gateway] started",
		"\t2026-09-23T10:11:12Z\t",
		"2026-09-23T10:11:12 ",
		"    at handler (index.js:1:2)",
		"12:00",
		"",
	} {
		assertParseLogTimestampMatchesReference(t, c)
	}
	assertParseLogTimestampMatchesReference(t, "not a time", "2026-09-23 10:11:12", "2026-09-23T00:00:00Z")
	assertParseLogTimestampMatchesReference(t, "", "x 2026-09-23T10:11:12Z", "2026-09-23T10:11:12Z trailing")
}

func TestParseLogTimestampRandomizedMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	pieces := []string{
		"2026", "-", "09", "-", "23", "T", "t", " ", "10", ":", "11", ":", "12",
		".", ",", "123", "Z", "z", "+", "08:00", "-07:00", "UTC", "\t", "é", "[gw]", "0", "9", "31", "24", "60",
	}
	for range 20000 {
		var b strings.Builder
		if rng.IntN(2) == 0 {
			b.WriteString("2026-09-23")
			b.WriteString([]string{"T", " ", "t"}[rng.IntN(3)])
			b.WriteString("10:11:12")
		}
		for range rng.IntN(6) {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		assertParseLogTimestampMatchesReference(t, b.String())
	}
}
