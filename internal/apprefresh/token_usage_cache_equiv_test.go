package apprefresh

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// referenceParseTokenUsage is the original map[string]any implementation of
// the per-line transcript decode, kept verbatim as the semantic oracle for
// parseTokenUsageFile: v1 decoding tolerates invalid UTF-8, keeps the last of
// duplicate keys, matches keys case-sensitively, and ignores wrong-typed
// fields rather than dropping the line.
func referenceParseTokenUsage(r io.Reader, size, mtime int64, loc *time.Location) (tokenUsageFileSummary, error) {
	summary := tokenUsageFileSummary{
		Size:            size,
		ModTimeUnixNano: mtime,
		Models:          map[string]TokenBucket{},
		Daily:           map[string]map[string]TokenBucket{},
	}
	reader := bufio.NewReaderSize(r, 256*1024)
	var sessionFirstTs, sessionLastTs time.Time
	for {
		line, err := reader.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			var obj map[string]any
			if json.Unmarshal(line, &obj) == nil {
				usage, model, ok := referenceUsageFromSessionEvent(obj)
				if ok {
					tt, _ := usage["totalTokens"].(float64)
					if tt > 0 && !strings.Contains(model, "delivery-mirror") {
						var costTotal float64
						if costObj, ok := usage["cost"].(map[string]any); ok {
							if t, ok := costObj["total"].(float64); ok {
								costTotal = t
							}
						}
						if costTotal < 0 {
							costTotal = 0
						}
						inp, _ := usage["input"].(float64)
						out, _ := usage["output"].(float64)
						cr, _ := usage["cacheRead"].(float64)
						cw, _ := usage["cacheWrite"].(float64)

						modelBucket := summary.Models[model]
						modelBucket.add(int(inp), int(out), int(cr), int(cw), int(tt), costTotal)
						summary.Models[model] = modelBucket
						summary.SessionCost += costTotal
						summary.SessionModel = model

						ts, _ := obj["timestamp"].(string)
						if ts != "" {
							if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
								t = t.In(loc)
								msgDate := t.Format("2006-01-02")
								if summary.Daily[msgDate] == nil {
									summary.Daily[msgDate] = map[string]TokenBucket{}
								}
								dailyBucket := summary.Daily[msgDate][model]
								dailyBucket.add(int(inp), int(out), int(cr), int(cw), int(tt), costTotal)
								summary.Daily[msgDate][model] = dailyBucket
								if sessionFirstTs.IsZero() {
									sessionFirstTs = t
								}
								sessionLastTs = t
							}
						}
					}
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return tokenUsageFileSummary{}, err
		}
	}
	if !sessionFirstTs.IsZero() {
		summary.SessionFirstUnixMs = sessionFirstTs.UnixMilli()
	}
	if !sessionLastTs.IsZero() {
		summary.SessionLastUnixMs = sessionLastTs.UnixMilli()
	}
	return summary, nil
}

func referenceUsageFromSessionEvent(obj map[string]any) (map[string]any, string, bool) {
	msg := asObj(obj["message"])
	if msg == nil {
		return nil, "", false
	}
	role, _ := msg["role"].(string)
	if role != "assistant" {
		return nil, "", false
	}
	usage := asObj(obj["usage"])
	if usage == nil {
		usage = asObj(msg["usage"])
	}
	if usage == nil {
		return nil, "", false
	}
	model, _ := msg["model"].(string)
	if model == "" {
		model = "unknown"
	}
	return usage, model, true
}

// assertParseMatchesReference writes content to a transcript and requires
// parseTokenUsageFile to produce exactly the reference summary.
func assertParseMatchesReference(t *testing.T, content []byte, loc *time.Location) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseTokenUsageFile(path, info, loc)
	if err != nil {
		t.Fatal(err)
	}
	want, err := referenceParseTokenUsage(bytes.NewReader(content), info.Size(), info.ModTime().UnixNano(), loc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("summary mismatch for %q\n got: %+v\nwant: %+v", content, got, want)
	}
}

// tokenUsageEdgeLines are transcript lines whose handling differs between
// lenient and strict JSON decoders. Each must aggregate exactly as the
// reference v1 map decode does.
var tokenUsageEdgeLines = []string{
	`{"timestamp":"2026-03-22T10:00:00Z","message":{"role":"assistant","model":"openai/gpt-5","usage":{"totalTokens":100,"input":60,"output":40,"cacheRead":5,"cacheWrite":7,"cost":{"total":0.1}}}}`,
	// duplicate keys at every level: last value wins
	`{"message":{"role":"assistant","model":"a","usage":{"totalTokens":5}},"message":{"role":"assistant","model":"b","usage":{"totalTokens":9,"input":1}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":5,"input":3},"usage":{"output":2}}}`,
	`{"message":{"role":"assistant","model":"m","model":7,"usage":{"totalTokens":5}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":5,"cost":{"total":1},"cost":{"input":2}}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":5,"cost":{"total":1,"total":"x"}}}}`,
	`{"message":{"role":"assistant","role":"user","usage":{"totalTokens":5}}}`,
	`{"message":{"role":"user","role":"assistant","usage":{"totalTokens":5}}}`,
	`{"timestamp":"2026-03-22T10:00:00Z","timestamp":7,"message":{"role":"assistant","usage":{"totalTokens":5}}}`,
	`{"usage":{"totalTokens":3},"usage":5,"message":{"role":"assistant","usage":{"totalTokens":11}}}`,
	// top-level usage takes precedence; non-object falls back to message.usage
	`{"usage":{"totalTokens":3,"input":1},"message":{"role":"assistant","usage":{"totalTokens":11}}}`,
	`{"usage":[1,2],"message":{"role":"assistant","usage":{"totalTokens":11}}}`,
	`{"usage":null,"message":{"role":"assistant","usage":{"totalTokens":11}}}`,
	// case-sensitive keys
	`{"Message":{"role":"assistant","usage":{"totalTokens":5}}}`,
	`{"message":{"Role":"assistant","usage":{"totalTokens":5}}}`,
	`{"message":{"role":"assistant","usage":{"TotalTokens":5}}}`,
	`{"message":{"role":"Assistant","usage":{"totalTokens":5}}}`,
	// wrong types are ignored, not fatal
	`{"timestamp":123,"message":{"role":"assistant","model":42,"usage":{"totalTokens":5,"input":"12","output":true,"cacheRead":null,"cacheWrite":[1],"cost":{"total":"x"}}}}`,
	`{"message":{"role":"assistant","model":"","usage":{"totalTokens":5,"cost":[]}}}`,
	`{"message":{"role":"assistant","model":null,"usage":{"totalTokens":5,"cost":null}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":"5"}}}`,
	`{"message":[],"usage":{"totalTokens":5}}`,
	`{"message":null}`,
	// numbers: fractional, exponent, negative, huge, overflow
	`{"message":{"role":"assistant","usage":{"totalTokens":0.5,"input":1.9,"output":-2.7,"cost":{"total":1e-3}}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":1E2,"input":2e1,"cost":{"total":-0.0}}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":-5}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":1e300,"input":1e19}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":5,"input":1e400}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":5}},"extra":1e400}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":5}},"extra":[{"deep":-1e999}]}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":5}},"extra":123456789012345678901234567890}`,
	// invalid UTF-8 and escapes in strings
	"{\"message\":{\"role\":\"assistant\",\"model\":\"gpt\xff\xfe-5\",\"usage\":{\"totalTokens\":5}}}",
	"{\"message\":{\"role\":\"assistant\",\"model\":\"m\",\"content\":\"bad \xc3\x28\",\"usage\":{\"totalTokens\":5}}}",
	`{"message":{"role":"assistant","model":"gpté\ud800-\"x\"\n","usage":{"totalTokens":5}}}`,
	`{"message":{"role":"assistant","model":"gpt","usage":{"totalTokens":5}}}`,
	`{"timestamp":"2026-03-22T10:00:00Z","message":{"role":"assistant","usage":{"totalTokens":5}}}`,
	"{\"timestamp\":\"2026-03-22T10:00:00Z\xff\",\"message\":{\"role\":\"assistant\",\"usage\":{\"totalTokens\":5}}}",
	"{\"message\":{\"role\":\"assistant\",\"usage\":{\"totalTokens\":5}},\"k\xff\":1}",
	`{"message":{"role":"assistant","usage":{"totalTokens":5}}}`,
	// delivery mirror and zero tokens are skipped
	`{"message":{"role":"assistant","model":"x/delivery-mirror","usage":{"totalTokens":5}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":0}}}`,
	// malformed or non-object lines are skipped
	`{"message":{"role":"assistant","usage":{"totalTokens":5}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":5}}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":5}}} {"a":1}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":5,}}}`,
	`{"message":{"role":"assistant","usage":{"totalTokens":05}}}`,
	`{'message':1}`,
	`[{"message":{"role":"assistant","usage":{"totalTokens":5}}}]`,
	`null`,
	`"str"`,
	`42`,
	"{\"message\":{\"role\":\"assistant\",\"model\":\"a\tb\",\"usage\":{\"totalTokens\":5}}}",
	// timestamps: offsets, fractions, invalid
	`{"timestamp":"2026-03-22T23:30:00.123456789+05:30","message":{"role":"assistant","usage":{"totalTokens":5}}}`,
	`{"timestamp":"2026-03-22 10:00:00","message":{"role":"assistant","usage":{"totalTokens":5}}}`,
	`{"timestamp":"","message":{"role":"assistant","usage":{"totalTokens":5}}}`,
	// padding whitespace, including Unicode spaces trimmed by bytes.TrimSpace
	"  \t{\"message\":{\"role\":\"assistant\",\"usage\":{\"totalTokens\":5}}}\r",
	" {\"message\":{\"role\":\"assistant\",\"usage\":{\"totalTokens\":5}}}\u0085",
}

func TestParseTokenUsageFile_MatchesReferenceOnEdgeLines(t *testing.T) {
	loc := time.FixedZone("plus2", 2*3600)
	for _, line := range tokenUsageEdgeLines {
		t.Run(line, func(t *testing.T) {
			assertParseMatchesReference(t, []byte(line+"\n"), loc)
		})
	}
	all := strings.Join(tokenUsageEdgeLines, "\n")
	t.Run("all lines in one file", func(t *testing.T) {
		assertParseMatchesReference(t, []byte(all), loc)
	})
	t.Run("long line beyond the read buffer", func(t *testing.T) {
		big := `{"timestamp":"2026-03-22T10:00:00Z","message":{"role":"assistant","model":"m","content":"` +
			strings.Repeat("x", 600*1024) + `","usage":{"totalTokens":5}}}`
		assertParseMatchesReference(t, []byte(all+"\n"+big+"\n"+all), loc)
	})
}

func TestParseTokenUsageFile_MatchesReferenceOnFixture(t *testing.T) {
	basePath, _ := writeTokenUsageFixture(t, t.TempDir(), 12, 1200)
	paths, err := filepath.Glob(filepath.Join(basePath, "*/sessions/*.jsonl*"))
	if err != nil || len(paths) != 12 {
		t.Fatalf("fixture paths = %d, err = %v", len(paths), err)
	}
	for _, loc := range []*time.Location{time.UTC, time.FixedZone("minus7", -7*3600)} {
		for _, p := range paths {
			content, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			assertParseMatchesReference(t, content, loc)
		}
	}
}

func FuzzParseTokenUsageLine_MatchesReference(f *testing.F) {
	for _, line := range tokenUsageEdgeLines {
		f.Add([]byte(line))
	}
	f.Fuzz(func(t *testing.T, line []byte) {
		assertParseMatchesReference(t, line, time.UTC)
	})
}
