package daemon

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// refuseBrowserPages keeps web pages away from /v1.
//
// The daemon listens on loopback TCP as well as its unix socket, and /v1 is
// unauthenticated: the socket's file permissions are the access control, and
// a TCP port has none. A page in any browser on this machine can send a
// "simple" cross-origin POST -- text/plain, no preflight -- to
// 127.0.0.1:<port>/v1/exec, and the handler decodes the body as JSON
// regardless of its declared type. So every page the user visits could run
// code as the user, limited only by guessing the port. /mcp already refused
// foreign origins, as the MCP specification requires; the /v1 routes beside
// it on the same listener did not.
//
// Two rules, the same ones the MCP transport applies:
//
//   - An Origin header is sent by browsers and by nothing else mcpx talks
//     to. One that is not loopback, not the daemon's own address and not in
//     transport.allowedOrigins is refused.
//   - Over TCP, while the daemon is bound only to loopback, the Host header
//     must name loopback. That is what stops DNS rebinding: a page that has
//     rebound its own name to 127.0.0.1 is same-origin to itself, sends no
//     Origin on a GET, and would otherwise read /v1/log.
//
// A request with no Origin over the socket -- every CLI, script and plugin
// call -- is untouched.
func (s *Server) refuseBrowserPages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !s.Origins.Allows(origin) {
			writeErr(w, http.StatusForbidden, fmt.Errorf("origin %s may not use this daemon; "+
				"add it to transport.allowedOrigins if it should", origin))
			return
		}
		if overTCP(r) && s.loopbackOnly() && !s.loopbackHost(r.Host) {
			writeErr(w, http.StatusForbidden, fmt.Errorf("host %q is not this daemon's; "+
				"a loopback daemon answers only to a loopback name", r.Host))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func overTCP(r *http.Request) bool {
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	return ok && addr.Network() == "tcp"
}

// loopbackOnly reports whether the TCP listener is bound to loopback, which
// is the only case where the set of names that reach it is known.
func (s *Server) loopbackOnly() bool {
	if s.Address == "" {
		return true
	}
	ip := net.ParseIP(strings.Trim(s.Address, "[]"))
	return strings.EqualFold(s.Address, "localhost") || (ip != nil && ip.IsLoopback())
}

func (s *Server) loopbackHost(host string) bool {
	h := host
	if split, _, err := net.SplitHostPort(host); err == nil {
		h = split
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
