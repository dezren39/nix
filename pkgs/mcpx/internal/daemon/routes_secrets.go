package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/dezren39/mcpx/internal/secrets"
)

// routesSecrets are the endpoints that give servers the secrets they need.
//
// None of the answers here carries a value, and none of the handlers logs a
// request body: the only thing a log line on these routes names is the secret.
// A decoder error is never returned to the caller, because it can quote a
// fragment of what it was given.
func (s *Server) routesSecrets(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/secrets", s.handleSecretsList)
	mux.HandleFunc("PUT /v1/secrets/{name}", s.handleSecretSet)
	mux.HandleFunc("DELETE /v1/secrets/{name}", s.handleSecretUnset)
	mux.HandleFunc("GET /v1/secret-requests", s.handleSecretRequestsList)
	mux.HandleFunc("GET /v1/secret-requests/{id}", s.handleSecretRequestGet)
	mux.HandleFunc("DELETE /v1/secret-requests/{id}", s.handleSecretRequestDecline)
	mux.HandleFunc("POST /v1/preflight", s.handlePreflight)
}

// noStore keeps an answer out of every cache between the daemon and the
// caller. The answers on these routes are the state of what people gave.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

func (s *Server) handleSecretsList(w http.ResponseWriter, _ *http.Request) {
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"secrets": s.reg.SecretList()})
}

func (s *Server) handleSecretSet(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	name := r.PathValue("name")
	if !secrets.ValidName(name) {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("%q is not an environment variable name", name))
		return
	}
	var req struct {
		Value *string `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.set.Bytes("http.bodyLimit"))).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New(`the body must be a JSON object with a "value" string`))
		return
	}
	if req.Value == nil || *req.Value == "" {
		writeErr(w, http.StatusBadRequest, errors.New("value is required and must not be empty"))
		return
	}
	if err := s.reg.SetSecret(name, *req.Value); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.logger.Printf("secret %s given a value at runtime", name)
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "set": true, "source": "runtime"})
}

func (s *Server) handleSecretUnset(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	name := r.PathValue("name")
	if !secrets.ValidName(name) {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("%q is not an environment variable name", name))
		return
	}
	if s.reg.UnsetSecret(name) {
		s.logger.Printf("runtime value for secret %s dropped", name)
	}
	// Whether something still answers for the name: the environment is not
	// touched by this call, so it may.
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "set": s.reg.inEnvironment(name)})
}

func (s *Server) handleSecretRequestsList(w http.ResponseWriter, _ *http.Request) {
	noStore(w)
	writeJSON(w, http.StatusOK, map[string]any{"requests": s.reg.SecretRequests()})
}

func (s *Server) handleSecretRequestGet(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id := r.PathValue("id")
	req, ok := s.reg.SecretRequest(id)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no secret request %q", id))
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (s *Server) handleSecretRequestDecline(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id := r.PathValue("id")
	req, ok := s.reg.DeclineSecretRequest(id)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no secret request %q", id))
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// handlePreflight reads a script and answers what it would need. It is the one
// place a script is looked at before it runs, and it runs nothing.
func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var req struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.set.Bytes("http.bodyLimit"))).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New(`the body must be a JSON object with a "source" string`))
		return
	}
	writeJSON(w, http.StatusOK, s.reg.Preflight(req.Source))
}
