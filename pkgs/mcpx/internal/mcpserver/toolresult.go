package mcpserver

import (
	"encoding/json"
	"strings"
)

// A tool result richer than a string.
//
// dispatch returns a string because almost every mcpx tool answers with text,
// and threading a content array through a dozen cases to serve one of them
// would make eleven signatures worse to serve the twelfth. The one tool that
// needs more is mcpx_exec: a script's artifacts must come back as
// resource_link blocks a host can fetch, not as base64 in the model's
// context, and that is the entire point of artifacts existing.
//
// So that one encodes its blocks into the string it already returns, and
// tools/call decodes it in exactly one place. The marker is U+001E, the ASCII
// record separator -- the same choice the script log channel made, for the
// same reason: it is not something a tool would produce by accident.
const resultMarker = "\x1emcpx-result\x1e"

type richResult struct {
	Text   string           `json:"text"`
	Blocks []map[string]any `json:"blocks,omitempty"`
	// Raw, when set, is the whole result verbatim; see EncodeRaw.
	Raw json.RawMessage `json:"raw,omitempty"`
}

func cutMarker(s string) (string, bool) { return strings.CutPrefix(s, resultMarker) }

// EncodeResult packs text and extra content blocks into one string.
//
// Returning the text unchanged when there are no blocks matters: it keeps
// every existing tool byte-for-byte what it was, so this cannot change what
// a client sees for anything that does not use it.
func EncodeResult(text string, blocks []map[string]any) string {
	if len(blocks) == 0 {
		return text
	}
	b, err := json.Marshal(richResult{Text: text, Blocks: blocks})
	if err != nil {
		return text
	}
	return resultMarker + string(b)
}

// decodeResult unpacks what EncodeResult produced.
func decodeResult(s string) (string, []map[string]any) {
	rest, ok := strings.CutPrefix(s, resultMarker)
	if !ok {
		return s, nil
	}
	var r richResult
	if json.Unmarshal([]byte(rest), &r) != nil {
		return s, nil
	}
	return r.Text, r.Blocks
}
