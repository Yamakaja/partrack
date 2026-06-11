package handler_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Yamakaja/partrack/internal/config"
	"github.com/Yamakaja/partrack/internal/distributor"
	"github.com/Yamakaja/partrack/internal/server"
	"github.com/Yamakaja/partrack/internal/testutil"
	"github.com/Yamakaja/partrack/internal/worker"
)

// ── fake IdP ────────────────────────────────────────────────────────────────

const (
	oidcTestClientID     = "test-client"
	oidcTestClientSecret = "test-client-secret"
)

// fakeIdP is a minimal OIDC provider: discovery document, JWKS, and a token
// endpoint that redeems pre-registered codes for RS256-signed ID tokens. The
// authorize endpoint is never called — tests parse the AuthCodeURL redirect
// and register a code directly with the claims (incl. nonce) it should mint.
type fakeIdP struct {
	server *httptest.Server
	key    *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]fakeIdPCode
}

type fakeIdPCode struct {
	claims    map[string]any
	challenge string // expected S256 code_challenge; "" skips PKCE enforcement
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	idp := &fakeIdP{key: key, codes: make(map[string]fakeIdPCode)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"issuer":                                idp.server.URL,
			"authorization_endpoint":                idp.server.URL + "/authorize",
			"token_endpoint":                        idp.server.URL + "/token",
			"jwks_uri":                              idp.server.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"keys": []map[string]any{{
				"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}},
		})
	})
	mux.HandleFunc("POST /token", idp.handleToken)

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// addCode registers an authorization code that the token endpoint will redeem
// for an ID token carrying the given claims, enforcing the PKCE challenge.
func (idp *fakeIdP) addCode(claims map[string]any, challenge string) string {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	code := fmt.Sprintf("code-%d", len(idp.codes)+1)
	idp.codes[code] = fakeIdPCode{claims: claims, challenge: challenge}
	return code
}

func (idp *fakeIdP) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	idp.mu.Lock()
	entry, ok := idp.codes[r.PostFormValue("code")]
	idp.mu.Unlock()
	if !ok {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	if entry.challenge != "" {
		verifier := r.PostFormValue("code_verifier")
		sum := sha256.Sum256([]byte(verifier))
		if verifier == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != entry.challenge {
			http.Error(w, `{"error":"invalid_grant","error_description":"PKCE verification failed"}`, http.StatusBadRequest)
			return
		}
	}

	now := time.Now()
	claims := map[string]any{
		"iss": idp.server.URL,
		"aud": oidcTestClientID,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
	maps.Copy(claims, entry.claims)
	idToken, err := idp.signJWT(claims)
	if err != nil {
		http.Error(w, "signing failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"access_token": "fake-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

// signJWT builds an RS256 JWT from the claims.
func (idp *fakeIdP) signJWT(claims map[string]any) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test"}) //nolint:errcheck
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// ── test environment ────────────────────────────────────────────────────────

// newOIDCEnv builds a partrack server configured with the fake IdP as
// provider "test". The shared TestMain server has no OIDC config, so OIDC
// tests run against their own instance (same database).
func newOIDCEnv(t *testing.T, idp *fakeIdP, autoProvision bool) *httptest.Server {
	t.Helper()
	cfg := &config.Config{
		Auth: config.AuthConfig{SessionSecret: "test-secret-for-testing-only-32b!"},
		App:  config.AppConfig{BaseURL: "http://partrack.test"},
		OIDC: []config.OIDCProvider{{
			Name:          "test",
			DisplayName:   "Test IdP",
			Issuer:        idp.server.URL,
			ClientID:      oidcTestClientID,
			ClientSecret:  oidcTestClientSecret,
			AutoProvision: autoProvision,
		}},
	}
	reg := distributor.NewRegistry()
	q := worker.New(1, testDB, reg)
	srv := server.New(cfg, testDB, q, reg)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts
}

// noRedirectClient returns an http.Client that surfaces redirects instead of
// following them.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// oidcInitiate hits the initiate endpoint and returns the parsed IdP
// authorization URL plus the oidc_state cookie.
func oidcInitiate(t *testing.T, ts *httptest.Server) (*url.URL, *http.Cookie) {
	t.Helper()
	resp, err := noRedirectClient().Get(ts.URL + "/api/v1/auth/oidc/test")
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("initiate: expected 302, got %d", resp.StatusCode)
	}
	authURL, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("initiate: bad Location: %v", err)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "oidc_state" {
			return authURL, c
		}
	}
	t.Fatal("initiate: no oidc_state cookie set")
	return nil, nil
}

// oidcCallback hits the callback endpoint with the given code/state and the
// state cookie, returning the response (redirect not followed).
func oidcCallback(t *testing.T, ts *httptest.Server, code, state string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet,
		ts.URL+"/api/v1/auth/oidc/test/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
	if err != nil {
		t.Fatalf("callback request: %v", err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// oidcLogin runs the full happy-path flow for the given identity claims and
// returns the callback response.
func oidcLogin(t *testing.T, ts *httptest.Server, idp *fakeIdP, claims map[string]any) *http.Response {
	t.Helper()
	authURL, cookie := oidcInitiate(t, ts)
	q := authURL.Query()

	merged := map[string]any{"nonce": q.Get("nonce")}
	maps.Copy(merged, claims)
	code := idp.addCode(merged, q.Get("code_challenge"))
	return oidcCallback(t, ts, code, q.Get("state"), cookie)
}

func expectLoginError(t *testing.T, resp *http.Response, code string) {
	t.Helper()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	want := "/login?error=" + code
	if loc := resp.Header.Get("Location"); loc != want {
		t.Fatalf("expected redirect to %q, got %q", want, loc)
	}
}

func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == "session" && c.Value != "" {
			return c
		}
	}
	return nil
}

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := testDB.QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return n
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestOIDC_InitiateSetsPKCEAndNonce(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	authURL, cookie := oidcInitiate(t, ts)
	q := authURL.Query()

	if q.Get("state") == "" || q.Get("nonce") == "" {
		t.Error("auth URL missing state or nonce")
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		t.Error("auth URL missing S256 PKCE challenge")
	}
	if got, want := q.Get("redirect_uri"), "http://partrack.test/api/v1/auth/oidc/test/callback"; got != want {
		t.Errorf("redirect_uri = %q, want %q (must come from app.base_url)", got, want)
	}
	// Cookie carries state.nonce.verifier; state must match the auth URL.
	parts := strings.SplitN(cookie.Value, ".", 3)
	if len(parts) != 3 || parts[0] != q.Get("state") {
		t.Errorf("oidc_state cookie malformed: %q", cookie.Value)
	}
}

func TestOIDC_AutoProvisionNewUser(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	resp := oidcLogin(t, ts, idp, map[string]any{
		"sub": "subject-1", "email": "new@example.com", "email_verified": true, "name": "New User",
	})
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("expected 302 to /, got %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	sc := sessionCookie(resp)
	if sc == nil {
		t.Fatal("no session cookie set")
	}

	// Session is usable.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/me", nil) //nolint:errcheck
	req.AddCookie(sc)
	meResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("me: expected 200, got %d", meResp.StatusCode)
	}
	var me struct {
		User struct {
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
		} `json:"user"`
	}
	if err := json.NewDecoder(meResp.Body).Decode(&me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.User.Email != "new@example.com" || me.User.DisplayName != "New User" {
		t.Errorf("unexpected user: %+v", me.User)
	}

	if n := countRows(t, `SELECT COUNT(*) FROM oidc_identities WHERE provider = 'test' AND subject = 'subject-1'`); n != 1 {
		t.Errorf("expected 1 oidc identity, got %d", n)
	}

	// Second login with the same subject reuses the account.
	resp2 := oidcLogin(t, ts, idp, map[string]any{
		"sub": "subject-1", "email": "new@example.com", "email_verified": true, "name": "New User",
	})
	if resp2.Header.Get("Location") != "/" {
		t.Fatalf("second login failed: redirect to %q", resp2.Header.Get("Location"))
	}
	if n := countRows(t, `SELECT COUNT(*) FROM users`); n != 1 {
		t.Errorf("expected 1 user after second login, got %d", n)
	}
}

func TestOIDC_AutoLinkExistingUserByVerifiedEmail(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	userID := testutil.CreateUser(t, testDB, "alice@example.com", "hunter2", "Alice")

	resp := oidcLogin(t, ts, idp, map[string]any{
		"sub": "alice-idp", "email": "alice@example.com", "email_verified": true, "name": "Alice IdP",
	})
	if resp.Header.Get("Location") != "/" {
		t.Fatalf("expected redirect to /, got %q", resp.Header.Get("Location"))
	}
	if n := countRows(t, `SELECT COUNT(*) FROM users`); n != 1 {
		t.Errorf("expected no new user (auto-link), got %d users", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM oidc_identities WHERE user_id = $1 AND provider = 'test' AND subject = 'alice-idp'`, userID); n != 1 {
		t.Errorf("identity not linked to existing user")
	}
}

func TestOIDC_RejectsUnverifiedEmail(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	testutil.CreateUser(t, testDB, "victim@example.com", "hunter2", "Victim")

	resp := oidcLogin(t, ts, idp, map[string]any{
		"sub": "attacker", "email": "victim@example.com", "email_verified": false,
	})
	expectLoginError(t, resp, "OIDC_EMAIL_UNVERIFIED")
	if n := countRows(t, `SELECT COUNT(*) FROM oidc_identities`); n != 0 {
		t.Error("identity must not be linked for unverified email")
	}
}

func TestOIDC_RejectsMissingEmail(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	resp := oidcLogin(t, ts, idp, map[string]any{"sub": "no-email", "email_verified": true})
	expectLoginError(t, resp, "OIDC_NO_EMAIL")
}

func TestOIDC_ProvisioningDisabled(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, false)

	resp := oidcLogin(t, ts, idp, map[string]any{
		"sub": "unknown", "email": "unknown@example.com", "email_verified": true,
	})
	expectLoginError(t, resp, "PROVISIONING_DISABLED")
	if n := countRows(t, `SELECT COUNT(*) FROM users`); n != 0 {
		t.Error("no user must be created when provisioning is disabled")
	}
}

func TestOIDC_StateMismatch(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	authURL, cookie := oidcInitiate(t, ts)
	q := authURL.Query()
	code := idp.addCode(map[string]any{
		"sub": "s", "email": "s@example.com", "email_verified": true, "nonce": q.Get("nonce"),
	}, q.Get("code_challenge"))

	resp := oidcCallback(t, ts, code, "forged-state", cookie)
	expectLoginError(t, resp, "INVALID_STATE")
}

func TestOIDC_MissingStateCookie(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	resp := oidcCallback(t, ts, "any-code", "any-state", nil)
	expectLoginError(t, resp, "INVALID_STATE")
}

func TestOIDC_NonceMismatch(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	authURL, cookie := oidcInitiate(t, ts)
	q := authURL.Query()
	// ID token minted with a different nonce — e.g. replayed from another login.
	code := idp.addCode(map[string]any{
		"sub": "s", "email": "s@example.com", "email_verified": true, "nonce": "stale-nonce",
	}, q.Get("code_challenge"))

	resp := oidcCallback(t, ts, code, q.Get("state"), cookie)
	expectLoginError(t, resp, "OIDC_VERIFY_FAILED")
}

func TestOIDC_InactiveUser(t *testing.T) {
	t.Cleanup(func() { testutil.Truncate(t, testDB) })
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	userID := testutil.CreateUser(t, testDB, "inactive@example.com", "pw", "Inactive")
	if _, err := testDB.Exec(t.Context(), `UPDATE users SET is_active = FALSE WHERE id = $1`, userID); err != nil {
		t.Fatalf("deactivate user: %v", err)
	}

	resp := oidcLogin(t, ts, idp, map[string]any{
		"sub": "inactive-idp", "email": "inactive@example.com", "email_verified": true,
	})
	expectLoginError(t, resp, "ACCOUNT_INACTIVE")
	if sessionCookie(resp) != nil {
		t.Error("no session may be issued for an inactive user")
	}
}

func TestOIDC_UnknownProvider(t *testing.T) {
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	resp, err := noRedirectClient().Get(ts.URL + "/api/v1/auth/oidc/nope")
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown provider, got %d", resp.StatusCode)
	}
}

func TestOIDC_ProvidersEndpoint(t *testing.T) {
	idp := newFakeIdP(t)
	ts := newOIDCEnv(t, idp, true)

	body, status := testutil.Do(t, ts, http.MethodGet, "/api/v1/auth/providers", nil, "")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	providers, ok := body["providers"].([]any)
	if !ok || len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %v", body["providers"])
	}
	p := providers[0].(map[string]any)
	if p["name"] != "test" || p["display_name"] != "Test IdP" {
		t.Errorf("unexpected provider entry: %v", p)
	}
}
