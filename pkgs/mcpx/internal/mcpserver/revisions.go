package mcpserver

import (
	"encoding/json"
	"strings"
)

// Oldest is the earliest revision mcpx serves.
//
// It is also what a request that declared nothing is treated as, because
// every shape that revision defines is understood by everything newer:
// guessing downward is recoverable, guessing upward is a frame the client
// cannot parse.
const Oldest = "2025-03-26"

// AtLeast compares revisions.
//
// Lexical comparison is correct because every revision is a date in
// ISO order, and that is not an accident of the naming -- the specification
// orders them this way on purpose. A test pins it so a future revision that
// breaks the assumption fails loudly rather than sorting wrong.
func AtLeast(version, floor string) bool {
	if version == "" {
		return false
	}
	return version >= floor
}

// Feature is something a revision either defines or does not.
//
// The point of naming them is that "send conservatively" has to be checkable.
// A field mcpx emits because the newest schema has it is a field an older
// client was never told about, and the failure is silent: the client ignores
// it, or rejects the frame, and nothing says which.
type Feature string

// The features whose presence differs across the revisions mcpx serves.
const (
	// FeatStructuredContent is CallToolResult.structuredContent, and the
	// outputSchema that describes it. Added in 2025-06-18.
	FeatStructuredContent Feature = "structuredContent"
	// FeatResourceLink is the resource_link content block. Added in
	// 2025-06-18; before it, a link had to be text or an embedded resource.
	FeatResourceLink Feature = "resource_link"
	// FeatAudio is the audio content block, added in 2025-03-26 -- which is
	// the oldest revision mcpx serves, so it is always available. Named
	// anyway, because "always true today" is a fact about the floor rather
	// than about the feature.
	FeatAudio Feature = "audio"
	// FeatElicitation is a server asking its client a question. Added in
	// 2025-06-18, form mode only.
	FeatElicitation Feature = "elicitation"
	// FeatElicitationURL is url-mode elicitation. Added in 2025-11-25.
	FeatElicitationURL Feature = "elicitationURL"
	// FeatTasks is tasks/*: core in 2025-11-25, an extension in 2026-07-28.
	FeatTasks Feature = "tasks"
	// FeatElicitationComplete is notifications/elicitation/complete, which
	// exists only in 2025-11-25: 2026-07-28 dropped it along with every
	// other server-to-client notification that is not a subscription.
	FeatElicitationComplete Feature = "elicitationComplete"
	// FeatResultType is the mandatory resultType on every result. 2026-07-28.
	FeatResultType Feature = "resultType"
	// FeatInputRequired is the input_required / inputResponses round trip,
	// which replaces server-initiated requests in the modern era.
	FeatInputRequired Feature = "inputRequired"
	// FeatSubscriptionsListen is the modern subscription stream, which
	// replaced resources/subscribe.
	FeatSubscriptionsListen Feature = "subscriptionsListen"
	// FeatResourceSubscribe is resources/subscribe and its unsubscribe,
	// removed in 2026-07-28.
	FeatResourceSubscribe Feature = "resourceSubscribe"
	// FeatLoggingSetLevel is logging/setLevel, removed in 2026-07-28 in
	// favour of servers emitting at a level of their own choosing.
	FeatLoggingSetLevel Feature = "loggingSetLevel"
	// FeatDiscover is server/discover, which only the modern era has and
	// which it makes mandatory.
	FeatDiscover Feature = "discover"
	// FeatInitialize is the handshake, which only the legacy era has.
	FeatInitialize Feature = "initialize"
	// FeatBatch is JSON-RPC batching: a server MUST accept batches in
	// 2025-03-26, and 2025-06-18 removed them.
	FeatBatch Feature = "batch"
)

// floors is the revision each feature arrived in. A feature with an empty
// floor is in every revision mcpx serves.
var floors = map[Feature]string{
	FeatStructuredContent:   "2025-06-18",
	FeatResourceLink:        "2025-06-18",
	FeatAudio:               "2025-03-26",
	FeatElicitation:         "2025-06-18",
	FeatElicitationURL:      "2025-11-25",
	FeatTasks:               "2025-11-25",
	FeatElicitationComplete: "2025-11-25",
	FeatResultType:          "2026-07-28",
	FeatInputRequired:       "2026-07-28",
	FeatSubscriptionsListen: "2026-07-28",
	FeatResourceSubscribe:   "2025-03-26",
	FeatLoggingSetLevel:     "2025-03-26",
	FeatDiscover:            "2026-07-28",
	FeatInitialize:          "2025-03-26",
	FeatBatch:               "2025-03-26",
}

// ceilings is the first revision that no longer defines a feature.
var ceilings = map[Feature]string{
	FeatElicitationComplete: "2026-07-28",
	FeatResourceSubscribe:   "2026-07-28",
	FeatLoggingSetLevel:     "2026-07-28",
	FeatInitialize:          "2026-07-28",
	FeatBatch:               "2025-06-18",
}

// Defines reports whether a revision defines a feature.
//
// This is what the specification says, which is not the same question as
// what mcpx accepts. mcpx accepts every method it implements from any
// revision -- a server offering more than its revision requires withholds
// nothing -- and consults this only when deciding what to *send*.
func Defines(version string, f Feature) bool {
	if floor, ok := floors[f]; ok && !AtLeast(version, floor) {
		return false
	}
	if ceiling, ok := ceilings[f]; ok && AtLeast(version, ceiling) {
		return false
	}
	return true
}

// downgrade rewrites a result into the shapes a revision defines.
//
// Everything mcpx builds internally is built in the newest shape, because
// building several and choosing is how the shapes drift apart. One function
// at the edge means an older client is served the same content, spelled in
// the vocabulary it has -- and a content block it cannot parse is not a
// degraded result, it is an unreadable one.
func downgrade(result any, version string) any {
	if Defines(version, FeatStructuredContent) && Defines(version, FeatResourceLink) &&
		Defines(version, FeatAudio) && Defines(version, FeatResultType) {
		// The newest revision defines everything mcpx builds, so there is
		// nothing to rewrite and nothing to pay for.
		return result
	}
	// Copied, not edited. The result may be a map a caller still holds -- a
	// task's stored result is handed out more than once -- and rewriting it
	// in place would downgrade it permanently for whoever reads it next.
	m, ok := copyMap(result)
	if !ok {
		return result
	}
	if !Defines(version, FeatStructuredContent) {
		// The data is not dropped: an older client would find nothing where
		// it looks, so it is rendered into the content array it does read.
		if sc, present := m["structuredContent"]; present {
			delete(m, "structuredContent")
			if b, err := json.MarshalIndent(sc, "", "  "); err == nil {
				m["content"] = appendContent(m["content"],
					map[string]any{"type": "text", "text": string(b)})
			}
		}
	}
	if c, present := m["content"]; present {
		m["content"] = downgradeContent(c, version)
	}
	if msgs, present := m["messages"]; present {
		m["messages"] = downgradeMessages(msgs, version)
	}
	if !Defines(version, FeatResultType) {
		// Harmless to a client that ignores unknown fields, and a lie to one
		// that does not: resultType is the modern era's signal, and sending
		// it to a legacy client says mcpx is speaking a revision it is not.
		delete(m, "resultType")
	}
	return m
}

// copyMap renders a result as a fresh map, sharing nothing with the original.
func copyMap(v any) (map[string]any, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}

func asMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}

func appendContent(existing any, block map[string]any) []any {
	list, _ := existing.([]any)
	return append(list, block)
}

// downgradeContent rewrites content blocks a revision does not define.
func downgradeContent(v any, version string) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		block, ok := asMap(item)
		if !ok {
			out = append(out, item)
			continue
		}
		typ, _ := block["type"].(string)
		switch {
		case typ == "resource_link" && !Defines(version, FeatResourceLink):
			// An embedded resource is the closest thing the older revisions
			// have, and unlike text it keeps the URI machine-readable.
			out = append(out, linkAsResource(block))
		case typ == "audio" && !Defines(version, FeatAudio):
			out = append(out, map[string]any{"type": "text",
				"text": describeBlob(block, "audio")})
		default:
			out = append(out, block)
		}
	}
	return out
}

// downgradeMessages applies the same rewriting inside a prompts/get reply,
// whose content blocks are the same vocabulary one level down.
func downgradeMessages(v any, version string) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		msg, ok := asMap(item)
		if !ok {
			out = append(out, item)
			continue
		}
		if c, present := msg["content"]; present {
			// A message's content is one block, not an array, so it is
			// wrapped for the shared rewriting and unwrapped after.
			if rewritten, ok := downgradeContent([]any{c}, version).([]any); ok && len(rewritten) == 1 {
				msg["content"] = rewritten[0]
			}
		}
		out = append(out, msg)
	}
	return out
}

func linkAsResource(block map[string]any) map[string]any {
	uri, _ := block["uri"].(string)
	name, _ := block["name"].(string)
	mime, _ := block["mimeType"].(string)
	if mime == "" {
		mime = "text/plain"
	}
	text := uri
	if name != "" {
		text = name + " — " + uri
	}
	return map[string]any{"type": "resource", "resource": map[string]any{
		"uri": uri, "mimeType": mime, "text": text}}
}

func describeBlob(block map[string]any, kind string) string {
	mime, _ := block["mimeType"].(string)
	data, _ := block["data"].(string)
	var b strings.Builder
	b.WriteString("(" + kind)
	if mime != "" {
		b.WriteString(" " + mime)
	}
	if data != "" {
		b.WriteString(", base64, elided)")
	} else {
		b.WriteString(")")
	}
	return b.String()
}

// Features is every feature the matrix covers, in a stable order.
var Features = []Feature{
	FeatInitialize, FeatDiscover, FeatResultType, FeatInputRequired,
	FeatStructuredContent, FeatResourceLink, FeatAudio,
	FeatElicitation, FeatElicitationURL, FeatElicitationComplete,
	FeatTasks, FeatResourceSubscribe, FeatSubscriptionsListen,
	FeatLoggingSetLevel, FeatBatch,
}

// FeatureMatrix is which revision defines which feature.
//
// Exposed so `GET /v1/protocol` can report it from the same table the code
// consults. A matrix written in a document and a matrix implemented in code
// agree on the day they are written and never again.
func FeatureMatrix() map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(Features))
	for _, f := range Features {
		row := make(map[string]bool, len(Supported))
		for _, v := range Supported {
			row[v] = Defines(v, f)
		}
		out[string(f)] = row
	}
	return out
}
