package mcpserver

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// stateSigner mints and checks the opaque requestState a modern client
// carries between the two halves of an interrupted request.
//
// The specification says the client MUST treat it as opaque, and says
// nothing about what a server should put in it. That silence is the whole
// risk: a bare call id is opaque to a well-behaved client and a handle
// anyone can guess to a hostile one, and resuming someone else's call is
// reading their tool results. So it is signed, bound to the request that
// started the call -- its method and a digest of its parameters, see
// requestBinding -- and expires.
//
// The key is per process and never persisted. A requestState does not need
// to survive a restart, because the call it names does not either.
type stateSigner struct {
	key []byte
	// ttl bounds how long a minted requestState stays resumable. Zero
	// means defaults.ProtoStateTTL.
	ttl time.Duration
}

func newStateSigner() *stateSigner {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Without a key nothing can be verified, and issuing tokens that
		// cannot be checked is worse than not issuing them: every mint
		// fails below instead.
		return &stateSigner{}
	}
	return &stateSigner{key: key}
}

type statePayload struct {
	Call    string `json:"c"`
	Binding string `json:"b"`
	Expires int64  `json:"e"`
	// Nonce makes every round's state its own. Without it two rounds of
	// one call minted within the same second were byte-identical, so a
	// client could not tell a new round from a repeat of the last one.
	Nonce string `json:"n"`
	// Rounds is how many times this call has come back asking, so far.
	// Each round of a 2026-07-28 exchange is a new request, so a count
	// kept per request restarted at zero on every retry, and an upstream
	// that never stopped asking was re-asked without limit. Carried in the
	// signed state, the client cannot reset it.
	Rounds int `json:"r,omitempty"`
}

// ErrNoStateKey means this process could not generate a signing key.
var ErrNoStateKey = errors.New("mcpx cannot sign a requestState")

func (s *stateSigner) mint(callID, binding string) (string, error) {
	return s.mintRound(callID, binding, 0)
}

// mintRound is mint recording the rounds asked so far.
func (s *stateSigner) mintRound(callID, binding string, rounds int) (string, error) {
	ttl := s.ttl
	if ttl <= 0 {
		ttl = defaults.ProtoStateTTL
	}
	if len(s.key) == 0 {
		return "", ErrNoStateKey
	}
	if binding == "" {
		return "", errors.New("a requestState must be bound to something")
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	b, err := json.Marshal(statePayload{Call: callID, Binding: binding,
		Expires: time.Now().Add(ttl).Unix(), Nonce: base64.RawURLEncoding.EncodeToString(nonce),
		Rounds: rounds})
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(b)
	return body + "." + s.sign(body), nil
}

func (s *stateSigner) sign(body string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *stateSigner) verify(token, binding string) (string, error) {
	id, _, err := s.verifyRound(token, binding)
	return id, err
}

// verifyRound is verify also returning the rounds the state records.
func (s *stateSigner) verifyRound(token, binding string) (string, int, error) {
	fail := func(err error) (string, int, error) { return "", 0, err }
	if len(s.key) == 0 {
		return fail(ErrNoStateKey)
	}
	body, mac, ok := strings.Cut(token, ".")
	if !ok {
		return fail(errors.New("requestState is malformed"))
	}
	// Constant time, because a comparison that stops at the first wrong byte
	// tells an attacker how much of a forgery was right.
	if !hmac.Equal([]byte(mac), []byte(s.sign(body))) {
		return fail(errors.New("requestState does not verify"))
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return fail(errors.New("requestState is malformed"))
	}
	var p statePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fail(errors.New("requestState is malformed"))
	}
	if p.Binding != binding {
		// The signature was ours, so this is a valid token presented on the
		// wrong request. Said plainly rather than as "invalid": the
		// difference is a bug in a client versus somebody replaying.
		return fail(errors.New("this requestState was issued for a different request"))
	}
	if time.Now().Unix() > p.Expires {
		return fail(fmt.Errorf("this requestState expired; the call it named is gone"))
	}
	return p.Call, p.Rounds, nil
}
