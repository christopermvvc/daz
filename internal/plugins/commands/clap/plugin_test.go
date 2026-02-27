package clap

import "testing"

func TestClapify_EmptyArgs(t *testing.T) {
	out, ok := clapify(nil)
	if ok {
		t.Fatalf("expected ok=false")
	}
	if out == "" {
		t.Fatalf("expected error message")
	}
}

func TestClapify_JoinsWords(t *testing.T) {
	out, ok := clapify([]string{"this", "is", "important"})
	if !ok {
		t.Fatalf("expected ok=true")
	}
	want := "this 👏 is 👏 important"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}
