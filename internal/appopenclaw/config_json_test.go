package appopenclaw

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestStripJSONCommentsAndTrailingCommas(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		want string
	}{
		{"plain json unchanged", `{"a":[1,2],"b":"c"}`, `{"a":[1,2],"b":"c"}`},
		{"line comment", "{\"a\":1 // note\n}", "{\"a\":1 \n}"},
		{"block comment", `{/* x */"a":1}`, `{ "a":1}`},
		{"multiline block comment", "{\"a\":1/* x\ny */}", "{\"a\":1 }"},
		{"trailing comma object", `{"a":1,}`, `{"a":1}`},
		{"trailing comma array", `[1,2,]`, `[1,2]`},
		{"trailing comma before whitespace", "{\"a\":[1,\n ],\n}", "{\"a\":[1\n ]\n}"},
		{"trailing comma before comment", "{\"a\":1, // c\n}", "{\"a\":1 \n}"},
		{"url in string kept", `{"u":"http://x/y"}`, `{"u":"http://x/y"}`},
		{"block marker in string kept", `{"u":"a/*b*/c"}`, `{"u":"a/*b*/c"}`},
		{"escaped quote in string", `{"u":"a\"//b",}`, `{"u":"a\"//b"}`},
		{"escaped backslash ends string", `{"u":"a\\"// c` + "\n}", "{\"u\":\"a\\\\\"\n}"},
		{"comma in string kept", `{"u":"a,}"}`, `{"u":"a,}"}`},
		{"unterminated block comment", `{"a":1}/* x`, `{"a":1} `},
		{"lone slash kept", `{"a":1}/`, `{"a":1}/`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(stripJSON5([]byte(tt.in))); got != tt.want {
				t.Fatalf("stripJSON5(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestUnmarshalConfig(t *testing.T) {
	var cfg struct {
		Gateway struct {
			URL   string `json:"url"`
			Token string `json:"token"`
		} `json:"gateway"`
	}
	data := []byte(`{
  // gateway settings
  "gateway": {
    "url": "http://127.0.0.1:18789/v1", /* local */
    "token": "abc//def",
  },
}`)
	if err := UnmarshalConfig(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway.URL != "http://127.0.0.1:18789/v1" || cfg.Gateway.Token != "abc//def" {
		t.Fatalf("cfg=%+v", cfg)
	}

	t.Run("strict json error kept for unsupported json5", func(t *testing.T) {
		var v map[string]any
		err := UnmarshalConfig([]byte(`{gateway: 1}`), &v)
		if err == nil {
			t.Fatal("unquoted keys accepted, want error")
		}
		if _, ok := errors.AsType[*json.SyntaxError](err); !ok {
			t.Fatalf("err=%T %v, want *json.SyntaxError", err, err)
		}
	})
}
