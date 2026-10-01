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
const Oldest = "2024-11-05"

// Headerless is the revision a legacy message is taken to speak when nothing
// said otherwise on the transport: a Streamable HTTP request with no
// MCP-Protocol-Version header and no session, and a stdio batch before
// initialize.
//
// Not Oldest, and the difference is deliberate. Oldest governs how results
// are *spelled* for a client that declared nothing -- guessing downward is
// the safe direction for shapes. But 2024-11-05 has no Streamable HTTP at
// all (its HTTP transport is HTTP+SSE, which mcpx does not host), so nothing
// arriving on /mcp can be a 2024-11-05 request that omitted the header; the
// 2025-06-18 and 2025-11-25 transport pages say a server SHOULD assume
// 2025-03-26 there, and 2026-07-28's says it MAY. And 2024-11-05's schema
// has no JSON-RPC batch type, so a line that opens with '[' comes from a
// client that believes batches exist -- 2025-03-26, which says a server
// MUST accept them. Judging that batch against 2024-11-05 would refuse the
// only revision that could have sent it.
const Headerless = "2025-03-26"

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
	// FeatAudio is the audio content block, added in 2025-03-26. A
	// 2024-11-05 client is given a text block describing it instead.
	FeatAudio Feature = "audio"
	// FeatToolAnnotations is Tool.annotations -- readOnlyHint and the rest.
	// Added in 2025-03-26.
	FeatToolAnnotations Feature = "toolAnnotations"
	// FeatCompletions is the completions server capability. completion/complete
	// itself is older, but 2024-11-05 has no capability to declare it with.
	FeatCompletions Feature = "completions"
	// FeatTitle is the display title on tools, prompts, prompt arguments,
	// resources and templates. Added in 2025-06-18.
	FeatTitle Feature = "title"
	// FeatIcons is the icons array on those same things. Added in 2025-11-25.
	FeatIcons Feature = "icons"
	// FeatExtensions is ServerCapabilities.extensions. 2026-07-28 only:
	// 2025-11-25's schema has no such field, so declaring an extension there
	// is sending a field the client was never told about.
	FeatExtensions Feature = "extensions"
	// FeatTasksExtension is io.modelcontextprotocol/tasks, which replaced
	// core tasks in 2026-07-28 with different wire shapes.
	FeatTasksExtension Feature = "tasksExtension"
	// FeatCacheable is ttlMs and cacheScope on list, read and discover
	// results. 2026-07-28.
	FeatCacheable Feature = "cacheable"
	// FeatElicitation is a server asking its client a question. Added in
	// 2025-06-18, form mode only.
	FeatElicitation Feature = "elicitation"
	// FeatElicitationURL is url-mode elicitation. Added in 2025-11-25.
	FeatElicitationURL Feature = "elicitationURL"
	// FeatTasks is core tasks/* -- the tasks capability, the task parameter,
	// tasks/list and a blocking tasks/result. 2025-11-25 only: 2026-07-28
	// moved tasks to an extension and removed both of those methods.
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
	// FeatPing is the ping utility, present from 2024-11-05 and absent from
	// the 2026-07-28 schema, which has no liveness probe: a modern request
	// is answered or it is not, and there is no session to keep alive.
	FeatPing Feature = "ping"
	// FeatProgressMessage is notifications/progress's message field.
	FeatProgressMessage Feature = "progressMessage"
)

// removedIn names the methods a revision no longer defines, so a peer on
// that revision is told method-not-found rather than served a method its own
// schema does not contain.
//
// This is the one place "accept liberally" does not apply, and the exception
// is the rule's own logic rather than a contradiction of it. Accepting more
// than a revision requires withholds nothing; answering a method the revision
// *removed* is different, because the removal is the specification telling
// the client to use the replacement. 2026-07-28 makes it explicit: a method
// the revision does not implement MUST be -32601 and, on HTTP, 404 -- and
// the 404 is load-bearing, because it is how a dual-era client distinguishes
// a modern server from a legacy one with no endpoint there.
//
// Legacy peers are unaffected: every one of these is still served to them.
var removedIn = map[string]Feature{
	"initialize":                         FeatInitialize,
	"ping":                               FeatPing,
	"logging/setLevel":                   FeatLoggingSetLevel,
	"resources/subscribe":                FeatResourceSubscribe,
	"resources/unsubscribe":              FeatResourceSubscribe,
	"notifications/elicitation/complete": FeatElicitationComplete,
}

// Removed reports whether a revision has dropped a method it once defined.
func Removed(version, method string) bool {
	f, ok := removedIn[method]
	if !ok {
		return false
	}
	return !Defines(version, f)
}

// floors is the revision each feature arrived in. A feature with no floor is
// in every revision mcpx serves, back to 2024-11-05.
var floors = map[Feature]string{
	FeatStructuredContent:   "2025-06-18",
	FeatResourceLink:        "2025-06-18",
	FeatAudio:               "2025-03-26",
	FeatProgressMessage:     "2025-03-26",
	FeatToolAnnotations:     "2025-03-26",
	FeatCompletions:         "2025-03-26",
	FeatTitle:               "2025-06-18",
	FeatIcons:               "2025-11-25",
	FeatExtensions:          "2026-07-28",
	FeatTasksExtension:      "2026-07-28",
	FeatCacheable:           "2026-07-28",
	FeatElicitation:         "2025-06-18",
	FeatElicitationURL:      "2025-11-25",
	FeatTasks:               "2025-11-25",
	FeatElicitationComplete: "2025-11-25",
	FeatResultType:          "2026-07-28",
	FeatInputRequired:       "2026-07-28",
	FeatSubscriptionsListen: "2026-07-28",
	FeatDiscover:            "2026-07-28",
	FeatBatch:               "2025-03-26",
}

// ceilings is the first revision that no longer defines a feature.
var ceilings = map[Feature]string{
	FeatTasks:               "2026-07-28",
	FeatElicitationComplete: "2026-07-28",
	FeatResourceSubscribe:   "2026-07-28",
	FeatLoggingSetLevel:     "2026-07-28",
	FeatInitialize:          "2026-07-28",
	FeatPing:                "2026-07-28",
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
	if Defines(version, FeatResultType) {
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
	if !Defines(version, FeatTitle) {
		// ResourceContents._meta arrived with the rest of _meta, in
		// 2025-06-18.
		if list, ok := m["contents"].([]any); ok {
			for _, c := range list {
				if cm, ok := c.(map[string]any); ok {
					delete(cm, "_meta")
				}
			}
		}
	}
	if msgs, present := m["messages"]; present {
		m["messages"] = downgradeMessages(msgs, version)
	}
	// The listed primitives carry fields that arrived one revision at a
	// time. Each is optional where it exists, so removing it loses a
	// display nicety and never meaning.
	for _, key := range []string{"tools", "prompts", "resources", "resourceTemplates"} {
		if list, present := m[key].([]any); present {
			m[key] = downgradeItems(list, version)
		}
	}
	// The modern-only envelope. Harmless to a client that ignores unknown
	// fields, and a lie to one that does not: each says mcpx is speaking a
	// revision it is not.
	delete(m, "resultType")
	delete(m, "ttlMs")
	delete(m, "cacheScope")
	if meta, ok := m["_meta"].(map[string]any); ok {
		delete(meta, MetaServerInfo)
		if len(meta) == 0 {
			delete(m, "_meta")
		}
	}
	return m
}

// downgradeItems strips per-item fields a revision does not define from a
// list of tools, prompts, resources or templates.
func downgradeItems(list []any, version string) []any {
	out := make([]any, 0, len(list))
	for _, item := range list {
		it, ok := asMap(item)
		if !ok {
			out = append(out, item)
			continue
		}
		if !Defines(version, FeatTitle) {
			// _meta on a listed item and Annotations.lastModified arrived
			// with title, in 2025-06-18.
			delete(it, "_meta")
			if an, ok := it["annotations"].(map[string]any); ok {
				delete(an, "lastModified")
			}
			delete(it, "title")
			if args, ok := it["arguments"].([]any); ok {
				for _, a := range args {
					if am, ok := a.(map[string]any); ok {
						delete(am, "title")
					}
				}
			}
		}
		if !Defines(version, FeatIcons) {
			delete(it, "icons")
			// execution.taskSupport arrived with core tasks.
			delete(it, "execution")
		}
		if !Defines(version, FeatStructuredContent) {
			delete(it, "outputSchema")
		}
		if !Defines(version, FeatToolAnnotations) {
			// Tool.annotations is 2025-03-26; resource annotations are
			// older, so only a tool's are removed.
			if _, isTool := it["inputSchema"]; isTool {
				delete(it, "annotations")
			}
		}
		out = append(out, it)
	}
	return out
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
	// The body is a text label, so its type is text/plain whatever the
	// link points at: an image/png resource whose text is a name and a URI
	// is a broken image (#206, CT-06).
	mime := "text/plain"
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
	FeatToolAnnotations, FeatCompletions, FeatTitle, FeatIcons,
	FeatElicitation, FeatElicitationURL, FeatElicitationComplete,
	FeatTasks, FeatTasksExtension, FeatExtensions, FeatCacheable,
	FeatResourceSubscribe, FeatSubscriptionsListen,
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
