package appopenclaw

import "encoding/json"

// UnmarshalConfig decodes an openclaw.json document into value. Strict JSON is
// tried first; on failure the document is retried with JSON5 comments and
// trailing commas removed, since hand-edited OpenClaw configs commonly carry
// both. Other JSON5 extensions (unquoted keys, single-quoted strings) are not
// supported, and the strict-JSON error is returned when both attempts fail.
func UnmarshalConfig(data []byte, value any) error {
	err := json.Unmarshal(data, value)
	if err == nil {
		return nil
	}
	if json.Unmarshal(stripJSON5(data), value) == nil {
		return nil
	}
	return err
}

// stripJSON5 removes // and /* */ comments and trailing commas before ] or }
// outside double-quoted strings. A block comment becomes one space so tokens it
// separated stay separated; a line comment keeps its terminating newline.
func stripJSON5(data []byte) []byte {
	return stripTrailingCommas(stripComments(data))
}

func stripComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		switch {
		case data[i] == '"':
			end := stringEnd(data, i)
			out = append(out, data[i:end]...)
			i = end
		case data[i] == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
		case data[i] == '/' && i+1 < len(data) && data[i+1] == '*':
			i += 2
			for i < len(data) && (data[i] != '*' || i+1 >= len(data) || data[i+1] != '/') {
				i++
			}
			i = min(i+2, len(data))
			out = append(out, ' ')
		default:
			out = append(out, data[i])
			i++
		}
	}
	return out
}

func stripTrailingCommas(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		switch data[i] {
		case '"':
			end := stringEnd(data, i)
			out = append(out, data[i:end]...)
			i = end
		case ',':
			j := i + 1
			for j < len(data) && isJSONSpace(data[j]) {
				j++
			}
			if j >= len(data) || (data[j] != ']' && data[j] != '}') {
				out = append(out, ',')
			}
			i++
		default:
			out = append(out, data[i])
			i++
		}
	}
	return out
}

// stringEnd returns the index just past the double-quoted string starting at
// start, honouring backslash escapes. An unterminated string runs to the end.
func stringEnd(data []byte, start int) int {
	for i := start + 1; i < len(data); i++ {
		switch data[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(data)
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
