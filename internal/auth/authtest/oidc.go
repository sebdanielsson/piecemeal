// Package authtest has a fake OpenID Connect provider for tests.
package authtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Provider is a minimal OIDC provider: discovery, keys and a token
// endpoint. Authorize issues a code for given claims, as if the user had
// signed in at the provider.
type Provider struct {
	*httptest.Server
	ClientID string
	key      *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]grant
}

type grant struct {
	claims    map[string]any
	nonce     string
	challenge string
}

// NewProvider starts a provider; it stops when the test ends.
func NewProvider(t testing.TB, clientID string) *Provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{ClientID: clientID, key: key, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer": p.URL, "authorization_endpoint": p.URL + "/authorize", "token_endpoint": p.URL + "/token",
			"jwks_uri": p.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"code_challenge_methods_supported": []string{"S256"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("POST /token", p.token)
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Close)
	return p
}

// Authorize returns a code for the sign-in the redirect URL started, with
// the given ID token claims (sub is required).
func (p *Provider) Authorize(t testing.TB, redirect *http.Request, claims map[string]any) string {
	t.Helper()
	q := redirect.URL.Query()
	if q.Get("client_id") != p.ClientID || q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
		t.Fatalf("bad authorization request: %s", redirect.URL)
	}
	code := base64.RawURLEncoding.EncodeToString([]byte(time.Now().String()))
	p.mu.Lock()
	defer p.mu.Unlock()
	p.codes[code] = grant{claims: claims, nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
	return code
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	g, ok := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithHeader("kid", "k"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": p.URL, "aud": p.ClientID, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "nonce": g.nonce,
	}
	for k, v := range g.claims {
		claims[k] = v
	}
	idToken, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idToken})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
