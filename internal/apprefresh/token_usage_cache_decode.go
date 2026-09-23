package apprefresh

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
)

// tokenUsageDecodeOptions keep the per-line decode as lenient as the
// encoding/json map decode it replaced. Transcripts are untrusted CLI/model
// output: invalid UTF-8 is replaced with U+FFFD and a duplicated member keeps
// its last value, instead of the whole line (and its usage) being dropped.
var tokenUsageDecodeOptions = []jsontext.Options{
	jsontext.AllowInvalidUTF8(true),
	jsontext.AllowDuplicateNames(true),
}

var errNotJSONObject = errors.New("transcript line is not a JSON object")

// usageFields is a transcript "usage" member. A field whose JSON value is not
// a number reads as zero, exactly as a failed float64 assertion did.
type usageFields struct {
	present                           bool // the member was a JSON object
	total, input, output, read, write float64
	costTotal                         float64
}

// usageEvent is the subset of a transcript line that token usage reads.
type usageEvent struct {
	// timestamp and model alias decoder buffers: valid until the next decode.
	timestamp   []byte // top-level "timestamp" when it is a string
	hasMessage  bool   // "message" was a JSON object
	isAssistant bool   // message.role == "assistant"
	model       []byte // message.model when it is a string
	topUsage    usageFields
	msgUsage    usageFields
}

// usage returns the counted usage of an assistant turn: a top-level "usage"
// object takes precedence over message.usage.
func (ev *usageEvent) usage() (usageFields, bool) {
	if !ev.hasMessage || !ev.isAssistant {
		return usageFields{}, false
	}
	if ev.topUsage.present {
		return ev.topUsage, true
	}
	return ev.msgUsage, ev.msgUsage.present
}

// usageLineDecoder streams one JSONL transcript line at a time through a
// reused jsontext.Decoder, reading only the members token usage needs.
// Unlike decoding into map[string]any, skipped values (message content,
// tool output) are validated but never materialized.
type usageLineDecoder struct {
	src bytes.Reader
	dec jsontext.Decoder

	// Unquoted string members, reused across lines.
	timestamp, role, model []byte
}

// decode reports the usage-relevant fields of line and whether line is one
// complete, valid JSON value — the lines encoding/json.Unmarshal accepted.
func (d *usageLineDecoder) decode(line []byte) (usageEvent, bool) {
	d.src.Reset(line)
	d.dec.Reset(&d.src, tokenUsageDecodeOptions...)
	ev, err := d.event()
	if err != nil {
		return usageEvent{}, false
	}
	// Unmarshal rejects anything after the first value.
	if _, err := d.dec.ReadToken(); !errors.Is(err, io.EOF) {
		return usageEvent{}, false
	}
	return ev, true
}

func (d *usageLineDecoder) event() (usageEvent, error) {
	var ev usageEvent
	tok, err := d.dec.ReadToken()
	if err != nil {
		return ev, err
	}
	if tok.Kind() != '{' {
		// null decoded to a nil map (no usage); anything else was an error.
		// Neither can carry usage, so the rest of the line is irrelevant.
		return ev, errNotJSONObject
	}
	for d.dec.PeekKind() != '}' {
		name, err := d.dec.ReadToken()
		if err != nil {
			return ev, err
		}
		switch name.String() {
		case "message":
			err = d.message(&ev)
		case "usage":
			ev.topUsage, err = d.usage()
		case "timestamp":
			d.timestamp, err = d.optionalString(d.timestamp)
			ev.timestamp = d.timestamp
		default:
			err = d.skip()
		}
		if err != nil {
			return ev, err
		}
	}
	_, err = d.dec.ReadToken()
	return ev, err
}

// message decodes a "message" member. A repeated member replaces every
// field of an earlier one, as a map assignment would.
func (d *usageLineDecoder) message(ev *usageEvent) error {
	ev.hasMessage, ev.isAssistant, ev.model, ev.msgUsage = false, false, nil, usageFields{}
	if d.dec.PeekKind() != '{' {
		return d.skip()
	}
	if _, err := d.dec.ReadToken(); err != nil {
		return err
	}
	ev.hasMessage = true
	for d.dec.PeekKind() != '}' {
		name, err := d.dec.ReadToken()
		if err != nil {
			return err
		}
		switch name.String() {
		case "role":
			d.role, err = d.optionalString(d.role)
			ev.isAssistant = string(d.role) == "assistant"
		case "model":
			d.model, err = d.optionalString(d.model)
			ev.model = d.model
		case "usage":
			ev.msgUsage, err = d.usage()
		default:
			err = d.skip()
		}
		if err != nil {
			return err
		}
	}
	_, err := d.dec.ReadToken()
	return err
}

func (d *usageLineDecoder) usage() (usageFields, error) {
	var u usageFields
	if d.dec.PeekKind() != '{' {
		return u, d.skip()
	}
	if _, err := d.dec.ReadToken(); err != nil {
		return u, err
	}
	u.present = true
	for d.dec.PeekKind() != '}' {
		name, err := d.dec.ReadToken()
		if err != nil {
			return u, err
		}
		switch name.String() {
		case "totalTokens":
			u.total, err = d.optionalNumber()
		case "input":
			u.input, err = d.optionalNumber()
		case "output":
			u.output, err = d.optionalNumber()
		case "cacheRead":
			u.read, err = d.optionalNumber()
		case "cacheWrite":
			u.write, err = d.optionalNumber()
		case "cost":
			u.costTotal, err = d.costTotal()
		default:
			err = d.skip()
		}
		if err != nil {
			return u, err
		}
	}
	_, err := d.dec.ReadToken()
	return u, err
}

// costTotal decodes usage.cost, returning cost.total when cost is an object
// and total is a number.
func (d *usageLineDecoder) costTotal() (float64, error) {
	if d.dec.PeekKind() != '{' {
		return 0, d.skip()
	}
	if _, err := d.dec.ReadToken(); err != nil {
		return 0, err
	}
	var total float64
	for d.dec.PeekKind() != '}' {
		name, err := d.dec.ReadToken()
		if err != nil {
			return 0, err
		}
		if name.String() == "total" {
			total, err = d.optionalNumber()
		} else {
			err = d.skip()
		}
		if err != nil {
			return 0, err
		}
	}
	_, err := d.dec.ReadToken()
	return total, err
}

// optionalString unquotes the next value into dst[:0] if it is a string,
// else returns dst[:0] empty.
func (d *usageLineDecoder) optionalString(dst []byte) ([]byte, error) {
	dst = dst[:0]
	if d.dec.PeekKind() != '"' {
		return dst, d.skip()
	}
	v, err := d.dec.ReadValue()
	if err != nil {
		return dst, err
	}
	// The decoder already validated v, so the only error AppendUnquote can
	// report is invalid UTF-8, which it has replaced with U+FFFD — the same
	// lenient result encoding/json produced.
	dst, _ = jsontext.AppendUnquote(dst, v)
	return dst, nil
}

// optionalNumber returns the next value if it is a number, else 0.
func (d *usageLineDecoder) optionalNumber() (float64, error) {
	if d.dec.PeekKind() != '0' {
		return 0, d.skip()
	}
	tok, err := d.dec.ReadToken()
	if err != nil {
		return 0, err
	}
	return tok.Float()
}

// skip consumes the next value. Numbers are range-checked because the map
// decode converted every number to float64 and rejected the whole line when
// one overflowed, even inside members token usage ignores.
func (d *usageLineDecoder) skip() error {
	depth := 0
	for {
		tok, err := d.dec.ReadToken()
		if err != nil {
			return err
		}
		switch tok.Kind() {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case '0':
			if _, err := tok.Float(); err != nil {
				return err
			}
		}
		if depth == 0 {
			return nil
		}
	}
}
