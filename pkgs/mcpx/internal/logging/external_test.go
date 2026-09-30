package logging

import "testing"

func TestExternalRecordTakesTheMessageFromTheFirstFieldPresent(t *testing.T) {
	for payload, want := range map[string]string{
		`{"msg":"a","message":"b","event":"c"}`: "a",
		`{"message":"b","event":"c"}`:           "b",
		`{"event":"c"}`:                         "c",
		`{"n":1}`:                               "",
	} {
		r, err := ExternalRecord([]byte(payload), "")
		if err != nil {
			t.Fatalf("%s: %v", payload, err)
		}
		if r.Msg != want {
			t.Errorf("%s: msg %q, want %q", payload, r.Msg, want)
		}
		if r.Attrs["external"] != true {
			t.Errorf("%s: every record must be marked external", payload)
		}
	}
}

func TestExternalRecordRefusesAnythingButAnObject(t *testing.T) {
	// null is the one that matters: it unmarshals without error into a nil
	// map, and writing to that panics -- inside the daemon, over HTTP.
	for _, payload := range []string{`null`, `[1,2]`, `"x"`, `3`, `not json`, ``} {
		if _, err := ExternalRecord([]byte(payload), ""); err == nil {
			t.Errorf("%q should be refused", payload)
		}
	}
}

func TestExternalRecordParsesTheLevel(t *testing.T) {
	r, err := ExternalRecord([]byte(`{"msg":"x"}`), "warn")
	if err != nil {
		t.Fatal(err)
	}
	if LevelName(r.Level) != "warn" {
		t.Errorf("level %s", LevelName(r.Level))
	}
	if _, err := ExternalRecord([]byte(`{"msg":"x"}`), "loud"); err == nil {
		t.Error("an unknown level should be refused")
	}
}
