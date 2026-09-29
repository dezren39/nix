package execsvc

import "testing"

func TestTheStderrTailIsBounded(t *testing.T) {
	// The tail rather than the head: a stack trace ends with the line that
	// matters, and a script that floods stderr floods it from the start.
	b := &tailBuffer{limit: 10}
	for i := 0; i < 100; i++ {
		_, _ = b.Write([]byte("0123456789"))
	}
	if got := b.String(); len(got) != 10 {
		t.Errorf("kept %d bytes, want the last 10", len(got))
	}
	b2 := &tailBuffer{limit: 4}
	if n, _ := b2.Write([]byte("abcdefgh")); n != 8 {
		t.Errorf("Write must report every byte accepted, got %d", n)
	}
	if b2.String() != "efgh" {
		t.Errorf("kept %q, want the tail", b2.String())
	}
}

func TestColourIsStrippedFromAnError(t *testing.T) {
	// Deno colours its errors whenever it thinks it has a terminal. This
	// reaches the caller as JSON, where escape codes are noise an agent has
	// to read past.
	in := "\x1b[0m\x1b[1m\x1b[31merror\x1b[0m: Uncaught SyntaxError: Illegal return statement"
	want := "error: Uncaught SyntaxError: Illegal return statement"
	if got := stripANSI(in); got != want {
		t.Errorf("stripANSI = %q, want %q", got, want)
	}
	if got := stripANSI("plain text"); got != "plain text" {
		t.Errorf("plain text should be untouched, got %q", got)
	}
}
