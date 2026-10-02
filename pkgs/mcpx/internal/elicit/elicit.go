// Package elicit holds questions that are waiting for an answer.
//
// Every other piece of mcpx state is either in a process or append-only in
// the log. A pending question is neither: it outlives the process that raised
// it, and it is mutated exactly once.
//
// The central decision is that a question is *state with a deadline* rather
// than a blocked function call. That is what allows the asker and the
// answerer to be different processes, on different days -- a call from CI
// answered from a laptop twenty minutes later. Everything else here follows
// from it.
package elicit

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"

	_ "modernc.org/sqlite"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Action is what a user did.
//
// Three, not two, and the distinction is normative. A server told "no" should
// offer an alternative; a server told "not now" may ask again later.
// Collapsing them makes tools worse in a way nobody traces back here.
type Action string

const (
	Accept  Action = "accept"
	Decline Action = "decline"
	Cancel  Action = "cancel"
)

// Mode is how the question is asked.
type Mode string

const (
	// Form asks for values matching a schema.
	Form Mode = "form"
	// URL sends the user somewhere out of band -- an authorisation flow, a
	// payment page -- so the answer never passes through a model.
	URL Mode = "url"
	// Sample is a request for a model completion rather than a question.
	// Stored in the same table because it has the same life: asked by a
	// server, answered by whatever drives mcpx, bounded by a deadline.
	Sample Mode = "sample"
	// Roots relays a server's roots/list to the client mcpx is serving, when
	// that client declared roots: in pass-through mode the roots that matter
	// are its own, not the ones configured on mcpx. Answered with a
	// ListRootsResult.
	Roots Mode = "roots"
)

// State is where a question is in its life.
type State string

const (
	Pending  State = "pending"
	Answered State = "answered"
	Expired  State = "expired"
)

// Audience is who should answer.
//
// The specification deliberately leaves this to the client: "If the client is
// an agent, it might decide how to handle the elicitation." So mcpx decides,
// and records the decision.
type Audience string

const (
	// ToAgent is the default, and usually right. The agent asked for
	// something; the question is part of that request; it has the context to
	// answer and can call a tool to do so.
	ToAgent Audience = "agent"
	// ToHuman is for anything an agent cannot know or should not hold: a
	// credential, a browser flow, a consent.
	ToHuman Audience = "human"
)

// Request is a question awaiting an answer.
type Request struct {
	ID        string          `json:"id"`
	Trace     string          `json:"trace,omitempty"`
	Parent    string          `json:"parent,omitempty"`
	Session   string          `json:"session,omitempty"`
	Server    string          `json:"server,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Mode      Mode            `json:"mode"`
	Message   string          `json:"message"`
	Schema    json.RawMessage `json:"requestedSchema,omitempty"`
	URL       string          `json:"url,omitempty"`
	Audience  Audience        `json:"audience"`
	Created   time.Time       `json:"created"`
	ExpiresAt time.Time       `json:"expiresAt"`
	State     State           `json:"state"`
	PID       int             `json:"pid,omitempty"`
	// Reason records why the audience was chosen, because a routing
	// decision nobody can inspect is one nobody can correct.
	Reason string `json:"reason,omitempty"`
}

// TTLRemaining is how long is left, never negative.
func (r Request) TTLRemaining() time.Duration {
	d := time.Until(r.ExpiresAt)
	if d < 0 {
		return 0
	}
	return d
}

// Answer is what came back.
type Answer struct {
	ID      string          `json:"id"`
	Action  Action          `json:"action"`
	Content json.RawMessage `json:"content,omitempty"`
	By      string          `json:"by,omitempty"`
	At      time.Time       `json:"at"`
	PID     int             `json:"pid,omitempty"`
}

// Broker stores questions and hands back answers.
type Broker struct {
	db *sql.DB

	mu      sync.Mutex
	waiters map[string][]chan Answer
	// poll is how often a waiter checks the table when the answer is coming
	// from another process. Human-scale, so a quarter second is invisible.
	poll time.Duration
}

// Open creates or opens the store.
func Open(path string) (*Broker, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating elicitation tables: %w", err)
	}
	return &Broker{db: db, waiters: map[string][]chan Answer{}, poll: defaults.ElicitPollInterval}, nil
}

func (b *Broker) Close() error { return b.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS elicitations (
  id         TEXT PRIMARY KEY,
  trace      TEXT, parent TEXT, session TEXT,
  server     TEXT, tool   TEXT,
  mode       TEXT NOT NULL,
  message    TEXT NOT NULL,
  schema_json TEXT,
  url        TEXT,
  audience   TEXT NOT NULL,
  reason     TEXT,
  created_ms INTEGER NOT NULL,
  expires_ms INTEGER NOT NULL,
  state      TEXT NOT NULL,
  pid        INTEGER
);
CREATE INDEX IF NOT EXISTS elicit_state   ON elicitations(state, expires_ms);
CREATE INDEX IF NOT EXISTS elicit_session ON elicitations(session);
CREATE INDEX IF NOT EXISTS elicit_trace   ON elicitations(trace);

CREATE TABLE IF NOT EXISTS elicitation_answers (
  id          TEXT PRIMARY KEY REFERENCES elicitations(id),
  action      TEXT NOT NULL,
  content     TEXT,
  answered_by TEXT,
  answered_ms INTEGER NOT NULL,
  pid         INTEGER
);
`

// NewID mints an identifier.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "elc-" + hex.EncodeToString(b[:])
}

// Open registers a question and returns its identifier.
func (b *Broker) OpenRequest(r Request) (Request, error) {
	if r.ID == "" {
		r.ID = NewID()
	}
	if r.Mode == "" {
		r.Mode = Form
	}
	if r.Created.IsZero() {
		r.Created = time.Now()
	}
	if r.ExpiresAt.IsZero() {
		r.ExpiresAt = r.Created.Add(defaults.ElicitTTL)
	}
	if r.Audience == "" {
		r.Audience, r.Reason = Route(r)
	}
	if r.PID == 0 {
		r.PID = os.Getpid()
	}
	r.State = Pending

	_, err := b.db.Exec(`INSERT INTO elicitations
		(id,trace,parent,session,server,tool,mode,message,schema_json,url,
		 audience,reason,created_ms,expires_ms,state,pid)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Trace, r.Parent, r.Session, r.Server, r.Tool, string(r.Mode),
		r.Message, string(r.Schema), r.URL, string(r.Audience), r.Reason,
		r.Created.UnixMilli(), r.ExpiresAt.UnixMilli(), string(r.State), r.PID)
	return r, err
}

// Await blocks until the question is answered or expires.
//
// An expiry answers cancel on the asker's behalf, because that is what
// expiry means: dismissed without an explicit choice. Answering decline
// would tell the server the user said no, which is a different and wrong
// thing.
func (b *Broker) Await(ctx context.Context, id string) (Answer, error) {
	if a, ok, err := b.Lookup(id); err != nil {
		return Answer{}, err
	} else if ok {
		return a, nil
	}

	ch := make(chan Answer, 1)
	b.mu.Lock()
	b.waiters[id] = append(b.waiters[id], ch)
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		remaining := b.waiters[id][:0]
		for _, c := range b.waiters[id] {
			if c != ch {
				remaining = append(remaining, c)
			}
		}
		b.waiters[id] = remaining
		b.mu.Unlock()
	}()

	// Both a channel and a poll. The channel covers an answer from this
	// process, which is instant; the poll covers one from another, which is
	// the interesting case and where the deadline has to be enforced anyway.
	ticker := time.NewTicker(b.poll)
	defer ticker.Stop()

	for {
		select {
		case a := <-ch:
			return a, nil
		case <-ctx.Done():
			return Answer{}, ctx.Err()
		case <-ticker.C:
			if a, ok, err := b.Lookup(id); err != nil {
				return Answer{}, err
			} else if ok {
				return a, nil
			}
			r, ok, err := b.Get(id)
			if err != nil {
				return Answer{}, err
			}
			if !ok {
				return Answer{}, fmt.Errorf("no elicitation %s", id)
			}
			if time.Now().After(r.ExpiresAt) {
				a := Answer{ID: id, Action: Cancel, By: "expiry", At: time.Now()}
				if err := b.Respond(a); err != nil {
					return Answer{}, err
				}
				return a, nil
			}
		}
	}
}

// Respond records an answer and wakes anything waiting.
func (b *Broker) Respond(a Answer) error {
	if a.At.IsZero() {
		a.At = time.Now()
	}
	if a.PID == 0 {
		a.PID = os.Getpid()
	}
	r, ok, err := b.Get(a.ID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no elicitation %s", a.ID)
	}
	if r.State != Pending {
		// Answering twice is not an error worth failing a command over; the
		// first answer stands and the caller is told so.
		return fmt.Errorf("%s was already %s", a.ID, r.State)
	}
	// JSON null is content by length and nothing by meaning: it unmarshals
	// into a nil map, and a null sampling answer crashed the daemon on its
	// way to the server. Refused here, the question stays open for a real one.
	if a.Action == Accept && (r.Mode == Form || r.Mode == Sample || r.Mode == Roots) &&
		(len(a.Content) == 0 || string(bytes.TrimSpace(a.Content)) == "null") {
		return errors.New("an accepted form elicitation needs content")
	}

	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state := Answered
	if a.By == "expiry" {
		state = Expired
	}
	if _, err := tx.Exec(`INSERT INTO elicitation_answers
		(id,action,content,answered_by,answered_ms,pid) VALUES (?,?,?,?,?,?)`,
		a.ID, string(a.Action), string(a.Content), a.By, a.At.UnixMilli(), a.PID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE elicitations SET state=? WHERE id=?`,
		string(state), a.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	b.mu.Lock()
	for _, ch := range b.waiters[a.ID] {
		select {
		case ch <- a:
		default:
		}
	}
	b.mu.Unlock()
	return nil
}

// Get returns one question.
func (b *Broker) Get(id string) (Request, bool, error) {
	rows, err := b.db.Query(selectRequests+` WHERE id=?`, id)
	if err != nil {
		return Request{}, false, err
	}
	defer rows.Close()
	out, err := scanRequests(rows)
	if err != nil || len(out) == 0 {
		return Request{}, false, err
	}
	return out[0], true, nil
}

// Lookup returns an answer if one exists.
func (b *Broker) Lookup(id string) (Answer, bool, error) {
	var a Answer
	var content sql.NullString
	var by sql.NullString
	var ms int64
	var pid sql.NullInt64
	err := b.db.QueryRow(
		`SELECT id,action,content,answered_by,answered_ms,pid FROM elicitation_answers WHERE id=?`,
		id).Scan(&a.ID, &a.Action, &content, &by, &ms, &pid)
	if errors.Is(err, sql.ErrNoRows) {
		return Answer{}, false, nil
	}
	if err != nil {
		return Answer{}, false, err
	}
	if content.Valid {
		a.Content = json.RawMessage(content.String)
	}
	a.By = by.String
	a.At = time.UnixMilli(ms)
	a.PID = int(pid.Int64)
	return a, true, nil
}

// Filter narrows a listing.
type Filter struct {
	Session  string
	Audience Audience
	State    State
	Limit    int
}

// Pending lists outstanding questions, expiring any that are overdue.
//
// Expiring on read rather than on a timer means there is no sweeper to go
// wrong, and a question is never reported as pending when it is not.
func (b *Broker) Pending(f Filter) ([]Request, error) {
	if err := b.expireOverdue(); err != nil {
		return nil, err
	}
	where := []string{"state='pending'"}
	var args []any
	if f.Session != "" {
		where = append(where, "session=?")
		args = append(args, f.Session)
	}
	if f.Audience != "" {
		where = append(where, "audience=?")
		args = append(args, string(f.Audience))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaults.ElicitPending
	}
	q := selectRequests + " WHERE " + strings.Join(where, " AND ") +
		" ORDER BY created_ms ASC LIMIT ?"
	rows, err := b.db.Query(q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRequests(rows)
}

func (b *Broker) expireOverdue() error {
	now := time.Now()
	rows, err := b.db.Query(
		`SELECT id FROM elicitations WHERE state='pending' AND expires_ms < ?`,
		now.UnixMilli())
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_ = b.Respond(Answer{ID: id, Action: Cancel, By: "expiry", At: now})
	}
	return nil
}

const selectRequests = `SELECT id,trace,parent,session,server,tool,mode,message,
	schema_json,url,audience,reason,created_ms,expires_ms,state,pid FROM elicitations`

func scanRequests(rows *sql.Rows) ([]Request, error) {
	var out []Request
	for rows.Next() {
		var r Request
		var trace, parent, session, server, tool, schemaJSON, url, reason sql.NullString
		var created, expires int64
		var pid sql.NullInt64
		if err := rows.Scan(&r.ID, &trace, &parent, &session, &server, &tool,
			&r.Mode, &r.Message, &schemaJSON, &url, &r.Audience, &reason,
			&created, &expires, &r.State, &pid); err != nil {
			return nil, err
		}
		r.Trace, r.Parent, r.Session = trace.String, parent.String, session.String
		r.Server, r.Tool, r.Reason = server.String, tool.String, reason.String
		r.URL = url.String
		if schemaJSON.Valid && schemaJSON.String != "" {
			r.Schema = json.RawMessage(schemaJSON.String)
		}
		r.Created = time.UnixMilli(created)
		r.ExpiresAt = time.UnixMilli(expires)
		r.PID = int(pid.Int64)
		// Route on read when the stored row has no audience. A question
		// written by an older version, or by something else holding the same
		// file, is unrouted rather than wrong -- and an unrouted question
		// that silently goes nowhere is worse than one routed late.
		if r.Audience == "" {
			r.Audience, r.Reason = Route(r)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Route decides who should answer, and says why.
//
// The default is the agent, which is both the common case and the useful one:
// the agent asked for something, the question is part of that request, and it
// has the context to answer. A human is pulled in only for what an agent
// cannot know or should not hold.
func Route(r Request) (Audience, string) {
	if r.Mode == Sample {
		return ToAgent, "sampling asks a model, and the agent driving mcpx has one"
	}
	if r.Mode == URL {
		return ToHuman, "url mode needs a browser and a person's consent"
	}
	fields := schemaFields(r.Schema)
	for _, f := range fields {
		lower := strings.ToLower(f.name)
		switch {
		case f.format == "password":
			return ToHuman, "a password field cannot be answered by a model"
		case containsAny(lower, "password", "secret", "token", "apikey", "api_key", "credential"):
			return ToHuman, "the field " + f.name + " looks like a credential"
		}
	}
	// A bare confirmation is consent, and consent is the one thing an agent
	// cannot give on somebody's behalf.
	if len(fields) == 1 && fields[0].typ == "boolean" &&
		containsAny(strings.ToLower(fields[0].name), "confirm", "approve", "consent", "proceed") {
		return ToHuman, "a confirmation is consent, which is not the agent's to give"
	}
	return ToAgent, "the agent has the context that raised this"
}

type field struct{ name, typ, format string }

func schemaFields(raw json.RawMessage) []field {
	if len(raw) == 0 {
		return nil
	}
	var doc struct {
		Properties map[string]struct {
			Type   string `json:"type"`
			Format string `json:"format"`
		} `json:"properties"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	out := make([]field, 0, len(doc.Properties))
	for name, p := range doc.Properties {
		out = append(out, field{name: name, typ: p.Type, format: p.Format})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func containsAny(hay string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
}
