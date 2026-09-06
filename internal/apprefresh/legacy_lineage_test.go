package apprefresh

import (
	"reflect"
	"testing"
)

func TestLegacyUsageLineageDoesNotCountRecoveryCopies(t *testing.T) {
	files := []string{"/agents/main/sessions/a.jsonl.deleted.2026-01", "/agents/main/sessions/a.jsonl", "/agents/main/sessions/a.jsonl.deleted.2026-02", "/agents/main/sessions/b.jsonl.deleted.2026-01", "/agents/main/sessions/b.jsonl.deleted.2026-02", "/agents/other/sessions/a.jsonl"}
	want := []string{"/agents/main/sessions/a.jsonl", "/agents/main/sessions/b.jsonl.deleted.2026-02", "/agents/other/sessions/a.jsonl"}
	if got := canonicalLegacyUsageFiles(files); !reflect.DeepEqual(got, want) {
		t.Fatalf("files=%v want=%v", got, want)
	}
}
