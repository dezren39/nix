package opencode

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// fixtureDB builds a minimal opencode-shaped database in a temp dir.
// Sessions: s1 (agent build, model m-a) with 3 messages, s2 (agent plan,
// model m-a) with 1 message, s3 (agent build, model m-b) with 2 messages.
func fixtureDB(t *testing.T, withMessages bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	stmts := []string{
		`CREATE TABLE session (id TEXT, project_id TEXT, directory TEXT, agent TEXT, model TEXT, cost REAL, tokens_input INTEGER, tokens_output INTEGER, time_created INTEGER, time_updated INTEGER)`,
		`CREATE TABLE part (id TEXT, session_id TEXT, message_id TEXT, time_created INTEGER)`,
		`INSERT INTO session VALUES ('s1','p1','/work','build','{"id":"m-a","providerID":"test"}',1.5,100,50,1700000000000,1700000000000)`,
		`INSERT INTO session VALUES ('s2','p1','/work','plan','{"id":"m-a","providerID":"test"}',0.5,40,10,1700000001000,1700000001000)`,
		`INSERT INTO session VALUES ('s3','p2','/else','build','{"id":"m-b","providerID":"test"}',2.0,200,80,1700000002000,1700000002000)`,
	}
	if withMessages {
		stmts = append(stmts,
			`CREATE TABLE message (id TEXT, session_id TEXT, time_created INTEGER)`,
			`INSERT INTO message VALUES ('m1','s1',1700000000000),('m2','s1',1700000001000),('m3','s1',1700000002000),('m4','s2',1700000003000),('m5','s3',1700000004000),('m6','s3',1700000005000)`,
		)
	} else {
		// No message table at all: statistics must degrade, not fail.
		stmts = append(stmts, `CREATE TABLE message (id TEXT, time_created INTEGER)`)
	}
	for _, s := range stmts {
		if _, err := raw.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func openFixture(t *testing.T, withMessages bool) *DB {
	t.Helper()
	db, err := Open(fixtureDB(t, withMessages))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Group.Messages was declared and merged (ByModel) but never populated:
// group() selected no message count, so every grouped statistic reported
// messages: 0 (#393).
func TestGroupCountsMessages(t *testing.T) {
	db := openFixture(t, true)

	byAgent, err := db.ByAgent(time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Group{}
	for _, g := range byAgent {
		got[g.Key] = g
	}
	if g := got["build"]; g.Sessions != 2 || g.Messages != 5 {
		t.Errorf("build group = %+v, want 2 sessions / 5 messages", g)
	}
	if g := got["plan"]; g.Sessions != 1 || g.Messages != 1 {
		t.Errorf("plan group = %+v, want 1 session / 1 message", g)
	}

	// ByModel merges the two m-a sessions: messages must sum across them.
	byModel, err := db.ByModel(time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	got = map[string]Group{}
	for _, g := range byModel {
		got[g.Key] = g
	}
	if g := got["test/m-a"]; g.Sessions != 2 || g.Messages != 4 {
		t.Errorf("test/m-a group = %+v, want 2 sessions / 4 messages", g)
	}
	if g := got["test/m-b"]; g.Sessions != 1 || g.Messages != 2 {
		t.Errorf("test/m-b group = %+v, want 1 session / 2 messages", g)
	}
}

func TestGroupWithoutMessageSessionID(t *testing.T) {
	db := openFixture(t, false)
	byAgent, err := db.ByAgent(time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(byAgent) != 2 {
		t.Fatalf("groups = %+v, want build and plan", byAgent)
	}
	for _, g := range byAgent {
		if g.Messages != 0 {
			t.Errorf("group %s Messages = %d, want 0 when message.session_id is absent", g.Key, g.Messages)
		}
	}
}
