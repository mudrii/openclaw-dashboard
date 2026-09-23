package appopenclaw

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// redactSeeds mirrors the redaction test tables so the fuzzer starts from
// every credential shape Redact is known to handle.
var redactSeeds = []string{
	"using sk-ABCD1234efgh5678 now",
	`{"apiKey":"xyz-123"}`,
	"password=hunter2",
	`{"access_token": "abc.def"}`,
	"totalTokens=100",
	"Authorization: Bearer abc123secret",
	"authorization: bearer abc123secret",
	"Authorization: Basic dXNlcjpwYXNz",
	`{\"token\":\"abc\"}`,
	`{\"password\": \"a b\"}`,
	`password='hunter2 with space' ok`,
	`{"secret":"a\"b c","n":1}`,
	`password="hunter2`,
	"openclaw gateway call --token abc123 --json",
	"run --gateway-token abc123",
	"run --token=abc123",
	"dial https://user:pw@example.com/path failed",
	"POST https://api.telegram.org/bot123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsawx/sendMessage",
	"using ghp_abcdefghijklmnopqrstuvwxyz0123",
	"slack xoxb-123456789012-abcdefABCDEF",
	"key AIzaSyA1234567890abcdefghijklmnopqrstuv end",
	"--max-tokens 512",
	"at 2026-09-23T10:11:12Z pid=4242 uptime=3600",
	"",
}

// FuzzRedact checks Redact never panics and is idempotent: a line that has
// already been redacted (stored in data.json, then shown again) must not be
// rewritten a second time.
func FuzzRedact(f *testing.F) {
	for _, s := range redactSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		once := Redact(in)
		if twice := Redact(once); twice != once {
			t.Fatalf("Redact not idempotent\ninput: %q\nonce:  %q\ntwice: %q", in, once, twice)
		}
	})
}

// knownSecretSamples are self-identifying credentials that must be removed
// wherever they appear in surrounding text.
var knownSecretSamples = []string{
	"sk-ABCD1234efgh5678",
	"ghp_abcdefghijklmnopqrstuvwxyz0123",
	"xoxb-123456789012-abcdefABCDEF",
	"AIzaSyA1234567890abcdefghijklmnopqrstuv",
	"123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsawx",
}

// FuzzRedactKnownSecret embeds a known credential between arbitrary prefix and
// suffix text separated by spaces and asserts it never survives redaction.
func FuzzRedactKnownSecret(f *testing.F) {
	for i, s := range redactSeeds {
		f.Add(s, s, uint8(i))
	}
	f.Fuzz(func(t *testing.T, prefix, suffix string, pick uint8) {
		secret := knownSecretSamples[int(pick)%len(knownSecretSamples)]
		in := prefix + " " + secret + " " + suffix
		if got := Redact(in); strings.Contains(got, secret) {
			t.Fatalf("known secret survived redaction\ninput: %q\noutput: %q", in, got)
		}
	})
}

// FuzzStripJSON5 checks the JSON5 fallback never changes the meaning of a
// document that is already strict JSON, and that UnmarshalConfig decodes
// strict JSON exactly like encoding/json.
func FuzzStripJSON5(f *testing.F) {
	for _, s := range []string{
		`{"a":1}`,
		`{"a":[1,2,],}`,
		`{"url":"http://x//y","c":"/* not a comment */"} // tail`,
		"{\n  // comment\n  \"a\": 1, /* block */ \"b\": [true,],\n}",
		`{"s":"a\"b//c","n":null}`,
		`[1, 2, 3]`,
		`"a,]"`,
		`{"a":"\\"}`,
		`/* unterminated`,
		`{"a":1}/`,
		``,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		stripped := stripJSON5(data)
		if len(stripped) > len(data) {
			t.Fatalf("stripJSON5 grew input: %d > %d bytes", len(stripped), len(data))
		}
		if !json.Valid(data) {
			var v any
			_ = UnmarshalConfig(data, &v) // must not panic
			return
		}
		if !bytes.Equal(stripped, data) {
			t.Fatalf("stripJSON5 rewrote strict JSON\ninput: %q\noutput: %q", data, stripped)
		}
		var want, got any
		if wantErr := json.Unmarshal(data, &want); wantErr != nil {
			// Syntactically valid but undecodable (e.g. a number beyond
			// float64): the fallback must not make it decodable.
			if err := UnmarshalConfig(data, &got); err == nil {
				t.Fatalf("UnmarshalConfig accepted %q that encoding/json rejects: %v", data, wantErr)
			}
			return
		}
		if err := UnmarshalConfig(data, &got); err != nil {
			t.Fatalf("UnmarshalConfig rejected strict JSON %q: %v", data, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("UnmarshalConfig decode differs\ninput: %q\ngot:  %#v\nwant: %#v", data, got, want)
		}
	})
}

// FuzzDecodeJSON checks the CLI-output decoder never panics and, when it
// succeeds on output that is already a single strict JSON value, agrees with
// encoding/json.
func FuzzDecodeJSON(f *testing.F) {
	for _, s := range []string{
		`{"a":1}`,
		"warning: something\n{\"a\":1}",
		"[x] {bad} {\"ok\":true}",
		`[1,2]`,
		`}{`,
		``,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var got any
		err := DecodeJSON(data, &got)
		if !json.Valid(data) {
			return
		}
		var want any
		if json.Unmarshal(data, &want) != nil {
			return // valid syntax but undecodable, e.g. a number beyond float64
		}
		if err != nil {
			t.Fatalf("DecodeJSON rejected strict JSON %q: %v", data, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("DecodeJSON differs\ninput: %q\ngot:  %#v\nwant: %#v", data, got, want)
		}
	})
}
