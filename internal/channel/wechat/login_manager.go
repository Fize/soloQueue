package wechat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	LoginCreatingQR           = "creating_qr"
	LoginAwaitingScan         = "awaiting_scan"
	LoginScanned              = "scanned"
	LoginAwaitingConfirmation = "awaiting_confirmation"
	LoginAwaitingVerification = "awaiting_verification"
	LoginConnected            = "connected"
	LoginAlreadyConnected     = "already_connected"
	LoginExpired              = "expired"
	LoginCancelled            = "cancelled"
	LoginFailed               = "failed"
)

var (
	ErrLoginNotFound = errors.New("wechat login session not found")
	ErrLoginConflict = errors.New("wechat login already active for account")
	ErrLoginCapacity = errors.New("too many active wechat login sessions")
)

type LoginClient interface {
	StartLogin(context.Context, []string) (QRCodeResponse, error)
	PollLogin(context.Context, string, string, string) (QRStatusResponse, error)
}

type LoginRequest struct {
	AccountID string `json:"accountId"`
	Name      string `json:"name"`
	BindType  string `json:"bindType"`
	BindAgent string `json:"bindAgent"`
}

type CredentialStore interface {
	LocalTokens() []string
	SaveWechatCredential(context.Context, LoginRequest, QRStatusResponse) error
}

type LoginSnapshot struct {
	SessionID string    `json:"sessionId"`
	AccountID string    `json:"accountId"`
	Status    string    `json:"status"`
	QRPayload string    `json:"qrPayload,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
	Message   string    `json:"message,omitempty"`
}

type loginSession struct {
	request    LoginRequest
	snapshot   LoginSnapshot
	qrCode     string
	baseURL    string
	verifyCode string
	cancel     context.CancelFunc
}

type LoginManager struct {
	mu       sync.RWMutex
	client   LoginClient
	store    CredentialStore
	sessions map[string]*loginSession
	starting map[string]struct{}
	ttl      time.Duration
	max      int
	closed   bool
}

func NewLoginManager(client LoginClient, store CredentialStore) *LoginManager {
	return &LoginManager{client: client, store: store, sessions: make(map[string]*loginSession), starting: make(map[string]struct{}), ttl: 8 * time.Minute, max: 5}
}

func (m *LoginManager) Start(ctx context.Context, req LoginRequest) (LoginSnapshot, error) {
	if req.AccountID == "" || (req.BindType != "" && req.BindType != "l1" && req.BindType != "l2") || (req.BindType == "l2" && req.BindAgent == "") {
		return LoginSnapshot{}, fmt.Errorf("invalid wechat login request")
	}
	if req.BindType == "" {
		req.BindType = "l1"
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return LoginSnapshot{}, errors.New("wechat login manager closed")
	}
	active := len(m.starting)
	for _, session := range m.sessions {
		if !terminalLoginStatus(session.snapshot.Status) {
			active++
		}
	}
	if active >= m.max {
		m.mu.Unlock()
		return LoginSnapshot{}, ErrLoginCapacity
	}
	if _, ok := m.starting[req.AccountID]; ok {
		m.mu.Unlock()
		return LoginSnapshot{}, ErrLoginConflict
	}
	for _, session := range m.sessions {
		if session.request.AccountID == req.AccountID && !terminalLoginStatus(session.snapshot.Status) {
			m.mu.Unlock()
			return LoginSnapshot{}, ErrLoginConflict
		}
	}
	m.starting[req.AccountID] = struct{}{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.starting, req.AccountID)
		m.mu.Unlock()
	}()

	var localTokens []string
	if m.store != nil {
		localTokens = m.store.LocalTokens()
	}
	qr, err := m.client.StartLogin(ctx, localTokens)
	if err != nil {
		return LoginSnapshot{}, err
	}
	if qr.QRCode == "" {
		return LoginSnapshot{}, errors.New("wechat login returned an empty QR code")
	}

	sessionID, err := randomLoginID()
	if err != nil {
		return LoginSnapshot{}, err
	}
	loginCtx, cancel := context.WithTimeout(context.Background(), m.ttl)
	payload := qr.QRCodeImageURL
	if payload == "" {
		payload = qr.QRCode
	}
	session := &loginSession{
		request: req,
		qrCode:  qr.QRCode,
		baseURL: DefaultBaseURL,
		cancel:  cancel,
		snapshot: LoginSnapshot{
			SessionID: sessionID,
			AccountID: req.AccountID,
			Status:    LoginAwaitingScan,
			QRPayload: payload,
			ExpiresAt: time.Now().Add(m.ttl),
			Message:   "Please scan the QR code using your mobile WeChat app",
		},
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		return LoginSnapshot{}, errors.New("wechat login manager closed")
	}
	m.sessions[sessionID] = session
	snapshot := session.snapshot
	m.mu.Unlock()
	go m.poll(loginCtx, sessionID)
	return snapshot, nil
}

func (m *LoginManager) Snapshot(sessionID string) (LoginSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session := m.sessions[sessionID]
	if session == nil {
		return LoginSnapshot{}, ErrLoginNotFound
	}
	return session.snapshot, nil
}

func (m *LoginManager) SubmitVerification(sessionID, code string) error {
	if code == "" {
		return errors.New("verification code is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[sessionID]
	if session == nil {
		return ErrLoginNotFound
	}
	if terminalLoginStatus(session.snapshot.Status) {
		return errors.New("wechat login session is no longer active")
	}
	session.verifyCode = code
	session.snapshot.Status = LoginScanned
	session.snapshot.Message = "Verifying..."
	return nil
}

func (m *LoginManager) Cancel(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[sessionID]
	if session == nil {
		return ErrLoginNotFound
	}
	if !terminalLoginStatus(session.snapshot.Status) {
		session.snapshot.Status = LoginCancelled
		session.snapshot.Message = "Login cancelled"
		session.snapshot.QRPayload = ""
		session.cancel()
	}
	return nil
}

func (m *LoginManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for _, session := range m.sessions {
		if !terminalLoginStatus(session.snapshot.Status) {
			session.snapshot.Status = LoginCancelled
			session.snapshot.QRPayload = ""
			session.cancel()
		}
	}
}

func (m *LoginManager) poll(ctx context.Context, sessionID string) {
	for {
		m.mu.Lock()
		session := m.sessions[sessionID]
		if session == nil || terminalLoginStatus(session.snapshot.Status) {
			m.mu.Unlock()
			return
		}
		baseURL, qrCode, verifyCode := session.baseURL, session.qrCode, session.verifyCode
		session.verifyCode = ""
		m.mu.Unlock()

		status, err := m.client.PollLogin(ctx, baseURL, qrCode, verifyCode)
		if err != nil {
			if ctx.Err() != nil {
				m.finishTimedOut(sessionID)
				return
			}
			select {
			case <-ctx.Done():
				m.finishTimedOut(sessionID)
				return
			case <-time.After(time.Second):
			}
			continue
		}
		if m.applyStatus(ctx, sessionID, status) {
			return
		}
		select {
		case <-ctx.Done():
			m.finishTimedOut(sessionID)
			return
		case <-time.After(time.Second):
		}
	}
}

func (m *LoginManager) applyStatus(ctx context.Context, sessionID string, status QRStatusResponse) bool {
	m.mu.Lock()
	session := m.sessions[sessionID]
	if session == nil || terminalLoginStatus(session.snapshot.Status) {
		m.mu.Unlock()
		return true
	}
	switch status.Status {
	case "wait":
		session.snapshot.Status = LoginAwaitingScan
		session.snapshot.Message = "Please scan the QR code using your mobile WeChat app"
	case "scaned":
		session.snapshot.Status = LoginAwaitingConfirmation
		session.snapshot.Message = "QR code scanned, please confirm on your mobile WeChat app"
	case "need_verifycode":
		session.snapshot.Status = LoginAwaitingVerification
		session.snapshot.Message = "Please enter the verification code displayed on your WeChat app"
	case "scaned_but_redirect":
		if status.RedirectHost != "" {
			session.baseURL = "https://" + status.RedirectHost
		}
		session.snapshot.Status = LoginAwaitingConfirmation
	case "confirmed":
		req := session.request
		m.mu.Unlock()
		if status.BotToken == "" || status.BotID == "" {
			m.setTerminal(sessionID, LoginFailed, "Incomplete login credentials returned by WeChat")
			return true
		}
		if m.store != nil {
			if err := m.store.SaveWechatCredential(ctx, req, status); err != nil {
				m.setTerminal(sessionID, LoginFailed, "Failed to save WeChat credentials")
				return true
			}
		}
		m.setTerminal(sessionID, LoginConnected, "WeChat account connected")
		return true
	case "binded_redirect":
		session.snapshot.Status = LoginAlreadyConnected
		session.snapshot.Message = "This WeChat account is already connected"
		session.snapshot.QRPayload = ""
		session.cancel()
		m.mu.Unlock()
		return true
	case "expired", "verify_code_blocked":
		session.snapshot.Status = LoginExpired
		session.snapshot.Message = "QR code expired, please try connecting again"
		session.snapshot.QRPayload = ""
		session.cancel()
		m.mu.Unlock()
		return true
	}
	m.mu.Unlock()
	return false
}

func (m *LoginManager) finishTimedOut(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.sessions[sessionID]
	if session != nil && !terminalLoginStatus(session.snapshot.Status) {
		session.snapshot.Status = LoginExpired
		session.snapshot.Message = "Login timed out"
		session.snapshot.QRPayload = ""
	}
}

func (m *LoginManager) setTerminal(sessionID, status, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session := m.sessions[sessionID]; session != nil {
		session.snapshot.Status = status
		session.snapshot.Message = message
		session.snapshot.QRPayload = ""
		session.cancel()
	}
}

func terminalLoginStatus(status string) bool {
	switch status {
	case LoginConnected, LoginAlreadyConnected, LoginExpired, LoginCancelled, LoginFailed:
		return true
	default:
		return false
	}
}

func randomLoginID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
