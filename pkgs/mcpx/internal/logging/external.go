package logging

import (
	"encoding/json"
	"fmt"
	"time"
)

// ExternalRecord turns a caller-supplied JSON object into a log record.
//
// It is the one definition of what `mcpx log record` and the daemon's
// POST /v1/log accept, so the two cannot drift: a record sent over the socket
// is indistinguishable from one sent by spawning the binary.
//
// Deliberately forgiving about shape. A caller that can produce JSON should
// not also have to learn a schema, so anything not recognised becomes an
// attribute, and a record with only a message is valid. The message is taken
// from msg, then message, then event.
//
// Every record is marked external, so a reader can tell what mcpx observed
// from what it was told. Without that distinction a synthetic record is
// indistinguishable from a measured one.
func ExternalRecord(payload []byte, level string) (Record, error) {
	attrs := map[string]any{}
	if err := json.Unmarshal(payload, &attrs); err != nil {
		return Record{}, fmt.Errorf("a record must be a JSON object: %w", err)
	}
	// `null` unmarshals without error and leaves the map nil, which would
	// panic on the first write below -- over HTTP, inside the daemon.
	if attrs == nil {
		return Record{}, fmt.Errorf("a record must be a JSON object, not null")
	}
	if level == "" {
		level = "info"
	}
	lvl, err := ParseLevel(level)
	if err != nil {
		return Record{}, err
	}
	msg, _ := attrs["msg"].(string)
	if msg == "" {
		msg, _ = attrs["message"].(string)
	}
	if msg == "" {
		msg, _ = attrs["event"].(string)
	}
	delete(attrs, "msg")
	delete(attrs, "message")
	attrs["external"] = true
	return Record{Time: time.Now(), Level: lvl, Msg: msg, Attrs: attrs}, nil
}
