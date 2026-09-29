// Package mcpauth resolves credentials for a server.
//
// The specification's position is short and worth repeating, because it is
// the opposite of what people assume:
//
//   - Authorization is OPTIONAL.
//   - stdio transports SHOULD NOT use OAuth. They "retrieve credentials from
//     the environment" -- which is to say, a child process is trusted because
//     you started it.
//   - HTTP transports SHOULD use OAuth 2.1 when the server is protected.
//
// So most servers need nothing. A local one inherits the environment. A
// remote one usually needs a token in a header, and only occasionally needs
// the full OAuth dance.
//
// This package covers the first two cases completely and gives the third a
// place to attach, because building OAuth before anything needs it would be
// guessing at a shape.
package mcpauth

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

// Auth declares how to authenticate to one server.
type Auth struct {
	// Type is bearer, basic, header, query, env or none. Empty means none.
	Type string `json:"type,omitempty"`

	// Token is used by bearer. Expanded, so ${GITHUB_TOKEN} works and the
	// secret itself never has to be in the file.
	Token string `json:"token,omitempty"`

	// Username and Password are used by basic.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// Header and Value are used by header, for the many services that
	// invented their own scheme before Bearer existed.
	Header string `json:"header,omitempty"`
	Value  string `json:"value,omitempty"`

	// Param and ParamValue are used by query, for the same reason.
	Param      string `json:"param,omitempty"`
	ParamValue string `json:"paramValue,omitempty"`

	// Env names variables to pass to a stdio server. The value may be
	// omitted, which means "take it from mcpx's own environment" -- the
	// common case, and the one that keeps a token out of the file entirely.
	Env map[string]string `json:"env,omitempty"`

	// OAuth configures the full flow, for a server that demands it.
	OAuth *OAuth `json:"oauth,omitempty"`
}

// OAuth is the 2.1 flow the specification describes for HTTP transports.
//
// Declared here so a configuration can express it and `mcpx doctor` can say
// what is missing. The flow itself needs a browser and a callback listener
// and is not yet implemented; a server requiring it reports that clearly
// rather than failing at the first request with a 401 nobody can interpret.
type OAuth struct {
	// ClientID may be a plain identifier or, preferably, an HTTPS URL
	// pointing at a Client ID Metadata Document.
	ClientID string `json:"clientId,omitempty"`
	// Scopes requested up front. The server's WWW-Authenticate challenge is
	// authoritative at runtime and may ask for more.
	Scopes []string `json:"scopes,omitempty"`
	// Resource is the canonical URI of the MCP server, required by RFC 8707
	// and sent whether or not the authorization server understands it.
	Resource string `json:"resource,omitempty"`
	// Redirect is where the authorization code comes back to.
	Redirect string `json:"redirect,omitempty"`
}

// Resolved is what a transport should actually apply.
type Resolved struct {
	Headers map[string]string
	Query   map[string]string
	Env     map[string]string
	// NeedsOAuth reports that this server is configured for a flow mcpx
	// cannot yet perform.
	NeedsOAuth bool
	// Missing names variables that were referenced and are not set, so the
	// failure can be reported before a request rather than as a 401.
	Missing []string
}

// Resolve turns a declaration into headers, query parameters and environment.
func (a *Auth) Resolve() (*Resolved, error) {
	out := &Resolved{
		Headers: map[string]string{},
		Query:   map[string]string{},
		Env:     map[string]string{},
	}
	if a == nil {
		return out, nil
	}

	expand := func(v string) string {
		if v == "" {
			return ""
		}
		expanded := os.Expand(v, func(name string) string {
			got := os.Getenv(name)
			if got == "" {
				out.Missing = append(out.Missing, name)
			}
			return got
		})
		return expanded
	}

	for k, v := range a.Env {
		if v == "" {
			// An empty value means "pass mine through". That is the shape
			// that keeps a secret out of the configuration file, so it is
			// worth supporting explicitly rather than by accident.
			if got := os.Getenv(k); got != "" {
				out.Env[k] = got
			} else {
				out.Missing = append(out.Missing, k)
			}
			continue
		}
		out.Env[k] = expand(v)
	}

	switch strings.ToLower(a.Type) {
	case "", "none":
		// Nothing, which is the common case and worth having a name for.

	case "bearer":
		token := expand(a.Token)
		if token != "" {
			out.Headers["Authorization"] = "Bearer " + token
		}

	case "basic":
		user, pass := expand(a.Username), expand(a.Password)
		if user != "" || pass != "" {
			out.Headers["Authorization"] = "Basic " +
				base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
		}

	case "header":
		if a.Header == "" {
			return nil, fmt.Errorf(`auth type "header" needs a header name`)
		}
		if v := expand(a.Value); v != "" {
			out.Headers[a.Header] = v
		}

	case "query":
		if a.Param == "" {
			return nil, fmt.Errorf(`auth type "query" needs a param name`)
		}
		// Permitted here because plenty of services require it, but the
		// specification forbids access tokens in a query string and it is
		// worth knowing that is what you are doing.
		if v := expand(a.ParamValue); v != "" {
			out.Query[a.Param] = v
		}

	case "env":
		// Everything was handled above; the type exists so a reader can see
		// the intent rather than inferring it from an otherwise empty block.

	case "oauth":
		out.NeedsOAuth = true

	default:
		return nil, fmt.Errorf("no auth type %q; bearer, basic, header, query, env, oauth or none",
			a.Type)
	}

	if a.OAuth != nil {
		out.NeedsOAuth = true
	}
	return out, nil
}

// Describe summarises an auth block without revealing anything.
//
// Used by `mcpx config` and `mcpx doctor`, both of which people paste into
// issues.
func (a *Auth) Describe() string {
	if a == nil || a.Type == "" || strings.EqualFold(a.Type, "none") {
		if a != nil && len(a.Env) > 0 {
			return fmt.Sprintf("env (%d variables)", len(a.Env))
		}
		return "none"
	}
	switch strings.ToLower(a.Type) {
	case "bearer":
		return "bearer " + redactRef(a.Token)
	case "basic":
		return "basic " + redactRef(a.Username)
	case "header":
		return "header " + a.Header + ": " + redactRef(a.Value)
	case "query":
		return "query " + a.Param + "=" + redactRef(a.ParamValue)
	case "oauth":
		return "oauth 2.1 (not yet implemented)"
	}
	return a.Type
}

// redactRef shows a ${VAR} reference but never a literal.
//
// A reference is safe and useful to print: it tells the reader which variable
// to set. A literal is the secret itself, and the whole point of printing
// anything is that somebody will paste it somewhere.
func redactRef(v string) string {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return "(unset)"
	case strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}"):
		return v
	case strings.HasPrefix(v, "$"):
		return v
	}
	return "(literal, redacted)"
}
