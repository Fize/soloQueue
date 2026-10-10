// Package chatgpt implements the Sign in with ChatGPT plan-usage flow and the
// Responses API transport used by the shared SoloQueue provider router.
package chatgpt

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	issuer       = "https://auth.openai.com"
	resource     = "https://api.openai.com/v1"
	authorizeURL = issuer + "/api/accounts/authorize"
	tokenURL     = issuer + "/api/accounts/oauth/token"
	modelsURL    = "https://api.openai.com/v1/models"
)

type Credentials struct {
	ClientID     string `json:"client_id"`
	HostID       string `json:"ext_agent_host_id"`
	Subject      string `json:"subject,omitempty"`
	Email        string `json:"email,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
}

type Manager struct {
	mu         sync.Mutex
	dir        string
	cred       Credentials
	pending    map[string]attempt
	client     *http.Client
	loginEpoch uint64
	credEpoch  uint64
	refreshing *refreshCall
	loggingOut *refreshCall
	persist    func(Credentials) error
}

var ErrLocalCredentialClear = errors.New("clear local ChatGPT credentials")

type attempt struct {
	state, nonce, verifier, redirectURI, clientID string
	newRegistration                               bool
	createdAt                                     time.Time
	generation                                    uint64
}

type refreshCall struct {
	done  chan struct{}
	token string
	err   error
}

var managers = struct {
	sync.Mutex
	m map[string]*Manager
}{m: make(map[string]*Manager)}

// ForWorkDir shares one credential manager between the runtime provider and
// its local settings API for the same work directory.
func ForWorkDir(workDir string) (*Manager, error) {
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}
	managers.Lock()
	defer managers.Unlock()
	if m := managers.m[abs]; m != nil {
		return m, nil
	}
	m, err := NewManager(filepath.Join(abs, "auth", "chatgpt"))
	if err != nil {
		return nil, err
	}
	managers.m[abs] = m
	return m, nil
}

func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create ChatGPT credential directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("protect ChatGPT credential directory: %w", err)
	}
	m := &Manager{dir: dir, pending: make(map[string]attempt), client: &http.Client{Timeout: 20 * time.Second}}
	m.persist = m.saveCredentials
	if err := m.load(); err != nil {
		return nil, err
	}
	if m.cred.HostID == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		m.cred.HostID = "urn:uuid:" + formatUUID(b)
		if err := m.save(); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func formatUUID(b []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (m *Manager) credentialPath() string { return filepath.Join(m.dir, "credentials.json") }
func (m *Manager) load() error {
	b, err := os.ReadFile(m.credentialPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read ChatGPT credentials: %w", err)
	}
	if err := json.Unmarshal(b, &m.cred); err != nil {
		return fmt.Errorf("decode ChatGPT credentials: %w", err)
	}
	if err := os.Chmod(m.credentialPath(), 0o600); err != nil {
		return fmt.Errorf("protect ChatGPT credentials: %w", err)
	}
	return nil
}
func (m *Manager) saveCredentials(cred Credentials) error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.dir, ".credentials-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, m.credentialPath())
}

func (m *Manager) save() error { return m.persist(m.cred) }

type Status struct {
	Connected bool   `json:"connected"`
	Email     string `json:"email,omitempty"`
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Status{Connected: m.cred.AccessToken != "" || m.cred.RefreshToken != "", Email: m.cred.Email}
}

func (m *Manager) StartLogin(redirectURI string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loggingOut != nil {
		return "", errors.New("ChatGPT sign-out is in progress")
	}
	m.loginEpoch++
	for key := range m.pending {
		delete(m.pending, key)
	}
	if !strings.HasPrefix(redirectURI, "http://127.0.0.1:") || !strings.HasSuffix(redirectURI, "/auth/callback") {
		return "", errors.New("ChatGPT sign-in requires a 127.0.0.1 /auth/callback URL")
	}
	state, err := randomURL(32)
	if err != nil {
		return "", err
	}
	nonce, err := randomURL(32)
	if err != nil {
		return "", err
	}
	verifier, err := randomURL(48)
	if err != nil {
		return "", err
	}
	challenge := sha256.Sum256([]byte(verifier))
	clientID := m.cred.ClientID
	newRegistration := clientID == ""
	if newRegistration {
		clientID = "dynamic_agent_client"
	}
	q := url.Values{
		"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirectURI},
		"scope":    {"openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"},
		"resource": {resource}, "state": {state}, "nonce": {nonce},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"ext_agent_host_id": {m.cred.HostID},
	}
	if newRegistration {
		q.Set("agent_name_hint", "SoloQueue")
	}
	if m.cred.IDToken != "" {
		q.Set("id_token_hint", m.cred.IDToken)
	}
	if m.cred.Email != "" {
		q.Set("login_hint", m.cred.Email)
	}
	m.pending[state] = attempt{state: state, nonce: nonce, verifier: verifier, redirectURI: redirectURI, clientID: clientID, newRegistration: newRegistration, createdAt: time.Now(), generation: m.loginEpoch}
	return authorizeURL + "?" + q.Encode(), nil
}

func randomURL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (m *Manager) Complete(ctx context.Context, state, code, returnedClientID string) error {
	m.mu.Lock()
	a, ok := m.pending[state]
	if ok {
		delete(m.pending, state)
	}
	m.mu.Unlock()
	if !ok || code == "" {
		return errors.New("unknown or expired ChatGPT authorization attempt")
	}
	if time.Since(a.createdAt) > 10*time.Minute {
		return errors.New("ChatGPT authorization attempt expired; start a new sign-in")
	}
	clientID := a.clientID
	if a.newRegistration {
		if returnedClientID == "" || returnedClientID == "dynamic_agent_client" {
			return errors.New("ChatGPT registration did not return an issued client ID")
		}
		clientID = returnedClientID
	} else if returnedClientID != "" && returnedClientID != clientID {
		return errors.New("ChatGPT returned a client ID different from the selected registration")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code}, "code_verifier": {a.verifier}, "redirect_uri": {a.redirectURI}, "resource": {resource}}
	tok, err := m.exchange(ctx, form)
	if err != nil {
		return err
	}
	claims, err := validateIDToken(ctx, m.client, tok.IDToken, clientID, a.nonce)
	if err != nil {
		return err
	}
	if !hasScope(tok.Scope, "chatgpt.tokens.use.direct") {
		return errors.New("ChatGPT sign-in succeeded without permission to use the ChatGPT plan")
	}
	if tok.ExpiresIn <= 0 {
		return errors.New("ChatGPT token response omitted a valid access-token lifetime")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loginEpoch != a.generation || m.loggingOut != nil {
		return errors.New("ChatGPT authorization attempt was superseded or cancelled")
	}
	if m.cred.Subject != "" && m.cred.ClientID == clientID && m.cred.Subject != claims.Subject {
		return errors.New("ChatGPT sign-in selected a different account for this registration")
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		return errors.New("ChatGPT token response omitted required credentials")
	}
	next := m.cred
	next.ClientID = clientID
	next.Subject = claims.Subject
	next.Email = claims.Email
	next.IDToken = tok.IDToken
	next.AccessToken = tok.AccessToken
	next.RefreshToken = tok.RefreshToken
	next.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix()
	if err := m.persist(next); err != nil {
		return fmt.Errorf("save ChatGPT credentials: %w", err)
	}
	m.cred = next
	m.credEpoch++
	return nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

type tokenEndpointError struct {
	Code   string
	Status int
}

func (e *tokenEndpointError) Error() string {
	if e.Code != "" {
		return "ChatGPT token endpoint rejected the request (" + e.Code + ")"
	}
	return fmt.Sprintf("ChatGPT token endpoint returned HTTP %d", e.Status)
}

func (m *Manager) exchange(ctx context.Context, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.client.Do(req)
	if err != nil {
		return tokenResponse{}, errors.New("ChatGPT token endpoint request failed")
	}
	defer resp.Body.Close()
	var t tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil {
		return t, fmt.Errorf("ChatGPT token endpoint returned HTTP %d with an unreadable response", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return t, &tokenEndpointError{Code: t.Error, Status: resp.StatusCode}
	}
	return t, nil
}
func hasScope(scope, want string) bool {
	for _, s := range strings.Fields(scope) {
		if s == want {
			return true
		}
	}
	return false
}

func (m *Manager) accessToken(ctx context.Context) (string, error) {
	for {
		m.mu.Lock()
		if logout := m.loggingOut; logout != nil {
			m.mu.Unlock()
			select {
			case <-logout.done:
				continue
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		break
	}
	if m.cred.AccessToken != "" && time.Now().Unix() < m.cred.ExpiresAt-60 {
		token := m.cred.AccessToken
		m.mu.Unlock()
		return token, nil
	}
	if m.cred.RefreshToken == "" {
		m.mu.Unlock()
		return "", errors.New("ChatGPT is not connected; sign in from Settings")
	}
	if call := m.refreshing; call != nil {
		m.mu.Unlock()
		select {
		case <-call.done:
			return call.token, call.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	m.refreshing = call
	base := m.cred
	credEpoch := m.credEpoch
	m.mu.Unlock()

	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {base.ClientID}, "refresh_token": {base.RefreshToken}, "resource": {resource}}
	tok, err := m.exchange(ctx, form)
	m.mu.Lock()
	defer m.mu.Unlock()
	defer func() {
		m.refreshing = nil
		close(call.done)
	}()
	if m.credEpoch != credEpoch || m.loggingOut != nil {
		call.err = errors.New("ChatGPT credential operation was superseded")
		return "", call.err
	}
	if err != nil {
		var endpointErr *tokenEndpointError
		if errors.As(err, &endpointErr) && isTerminalRefreshError(endpointErr.Code) {
			next := clearAccountCredentials(m.cred)
			if saveErr := m.persist(next); saveErr != nil {
				call.err = fmt.Errorf("ChatGPT session expired and local credential cleanup failed: %w", saveErr)
				return "", call.err
			}
			m.cred = next
			call.err = errors.New("ChatGPT session expired or was revoked; sign in again from Settings")
			return "", call.err
		}
		call.err = err
		return "", err
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		call.err = errors.New("ChatGPT refresh response omitted required credentials")
		return "", call.err
	}
	if tok.ExpiresIn <= 0 {
		call.err = errors.New("ChatGPT refresh response omitted a valid access-token lifetime")
		return "", call.err
	}
	next := m.cred
	next.AccessToken = tok.AccessToken
	next.RefreshToken = tok.RefreshToken
	if tok.IDToken != "" {
		next.IDToken = tok.IDToken
	}
	next.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix()
	if err := m.persist(next); err != nil {
		call.err = fmt.Errorf("save refreshed ChatGPT credentials: %w", err)
		return "", call.err
	}
	m.cred = next
	m.credEpoch++
	call.token = next.AccessToken
	return call.token, nil
}

func isTerminalRefreshError(code string) bool {
	switch code {
	case "invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused":
		return true
	default:
		return false
	}
}

func clearAccountCredentials(cred Credentials) Credentials {
	cred.IDToken = ""
	cred.AccessToken = ""
	cred.RefreshToken = ""
	cred.ExpiresAt = 0
	cred.Subject = ""
	cred.Email = ""
	return cred
}

func (m *Manager) Logout(ctx context.Context) (resultErr error) {
	m.mu.Lock()
	if call := m.loggingOut; call != nil {
		m.mu.Unlock()
		select {
		case <-call.done:
			return call.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	m.loggingOut = call
	defer func() {
		m.mu.Lock()
		call.err = resultErr
		m.loggingOut = nil
		close(call.done)
		m.mu.Unlock()
	}()
	m.loginEpoch++
	m.credEpoch++
	credEpoch := m.credEpoch
	for key := range m.pending {
		delete(m.pending, key)
	}
	base := m.cred
	m.mu.Unlock()
	var revokeErr error
	if base.RefreshToken != "" {
		var metadata oidcConfig
		if err := getJSON(ctx, m.client, issuer+"/.well-known/openid-configuration", &metadata); err != nil || metadata.RevocationEndpoint == "" {
			revokeErr = errors.New("ChatGPT revocation endpoint could not be resolved")
		}
		form := url.Values{"token": {base.RefreshToken}, "token_type_hint": {"refresh_token"}, "client_id": {base.ClientID}}
		var err error
		var req *http.Request
		if revokeErr == nil {
			req, err = http.NewRequestWithContext(ctx, http.MethodPost, metadata.RevocationEndpoint, strings.NewReader(form.Encode()))
		}
		if err == nil && revokeErr == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			var resp *http.Response
			resp, err = m.client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					revokeErr = fmt.Errorf("ChatGPT session revocation returned HTTP %d", resp.StatusCode)
				}
			}
		}
		if err != nil {
			revokeErr = errors.New("ChatGPT session revocation could not be confirmed")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.credEpoch != credEpoch {
		return errors.New("ChatGPT logout was superseded by a newer sign-in")
	}
	next := clearAccountCredentials(m.cred)
	if err := m.persist(next); err != nil {
		return fmt.Errorf("%w: %v", ErrLocalCredentialClear, err)
	}
	m.cred = next
	return revokeErr
}

type idClaims struct {
	Issuer          string `json:"iss"`
	Subject         string `json:"sub"`
	Audience        any    `json:"aud"`
	Exp             int64  `json:"exp"`
	Nonce           string `json:"nonce"`
	Email           string `json:"email"`
	AuthorizedParty string `json:"azp"`
}
type jwks struct {
	Keys []struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Alg string `json:"alg"`
		N   string `json:"n"`
		E   string `json:"e"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		Y   string `json:"y"`
	} `json:"keys"`
}
type oidcConfig struct {
	JWKSURI            string `json:"jwks_uri"`
	RevocationEndpoint string `json:"revocation_endpoint"`
}

func validateIDToken(ctx context.Context, client *http.Client, token, audience, nonce string) (idClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return idClaims{}, errors.New("ChatGPT ID token is malformed")
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &hdr) != nil {
		return idClaims{}, errors.New("ChatGPT ID token header is invalid")
	}
	cb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return idClaims{}, errors.New("ChatGPT ID token claims are invalid")
	}
	var c idClaims
	if err := json.Unmarshal(cb, &c); err != nil {
		return c, errors.New("ChatGPT ID token claims are invalid")
	}
	if c.Issuer != issuer || c.Subject == "" || c.Exp <= time.Now().Unix() || !audienceContains(c.Audience, audience) || (audienceCount(c.Audience) > 1 && c.AuthorizedParty != audience) || subtle.ConstantTimeCompare([]byte(c.Nonce), []byte(nonce)) != 1 {
		return c, errors.New("ChatGPT ID token validation failed")
	}
	var discovery oidcConfig
	if err := getJSON(ctx, client, issuer+"/.well-known/openid-configuration", &discovery); err != nil || discovery.JWKSURI == "" {
		return c, errors.New("could not load OpenAI signing keys")
	}
	var keys jwks
	if err := getJSON(ctx, client, discovery.JWKSURI, &keys); err != nil {
		return c, errors.New("could not load OpenAI signing keys")
	}
	for _, k := range keys.Keys {
		if k.Kid == hdr.Kid {
			if k.Alg != "" && k.Alg != hdr.Alg {
				continue
			}
			if verifyJWTSignature(hdr.Alg, k, parts[0]+"."+parts[1], parts[2]) {
				return c, nil
			}
		}
	}
	return c, errors.New("ChatGPT ID token signature validation failed")
}
func audienceContains(v any, want string) bool {
	switch x := v.(type) {
	case string:
		return x == want
	case []any:
		for _, a := range x {
			if s, ok := a.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}
func audienceCount(v any) int {
	switch x := v.(type) {
	case string:
		return 1
	case []any:
		return len(x)
	default:
		return 0
	}
}
func getJSON(ctx context.Context, c *http.Client, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	r, err := c.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", r.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(out)
}
func verifyJWTSignature(alg string, k struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}, input, sig string) bool {
	sb, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return false
	}
	digest := sha256.Sum256([]byte(input))
	switch alg {
	case "RS256":
		if k.Kty != "RSA" {
			return false
		}
		n, e1 := base64.RawURLEncoding.DecodeString(k.N)
		e, e2 := base64.RawURLEncoding.DecodeString(k.E)
		if e1 != nil || e2 != nil {
			return false
		}
		exp := new(big.Int).SetBytes(e).Int64()
		return rsa.VerifyPKCS1v15(&rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(exp)}, crypto.SHA256, digest[:], sb) == nil
	case "ES256":
		if k.Kty != "EC" || k.Crv != "P-256" || len(sb) != 64 {
			return false
		}
		x, _ := base64.RawURLEncoding.DecodeString(k.X)
		y, _ := base64.RawURLEncoding.DecodeString(k.Y)
		if x == nil || y == nil {
			return false
		}
		return ecdsa.Verify(&ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, digest[:], new(big.Int).SetBytes(sb[:32]), new(big.Int).SetBytes(sb[32:]))
	default:
		return false
	}
}
