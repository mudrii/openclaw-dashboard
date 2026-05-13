package apprefresh

import "testing"

func TestTrimLabel_StripsIDVariants(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"My Team id:-123456789", "My Team"},
		{"My Team ID=-1", "My Team"},
		{"My Team Id: -42", "My Team"},
		{"My Team id - 42", "My Team"},
		{"My Team id:42 group chat", "My Team group chat"},
		{"", ""},
		{"no id here", "no id here"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := TrimLabel(tc.in)
			if got != tc.want {
				t.Errorf("TrimLabel(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
