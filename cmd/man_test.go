package cmd

import "testing"

func TestEscapeTextSkipsCodeBlocks(t *testing.T) {
	got := escapeText("hapiq download <id>\n\tsource <(x)\n    a > b")
	want := "hapiq download \\<id\\>\n\tsource <(x)\n    a > b"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
