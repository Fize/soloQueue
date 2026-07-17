package wechat

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeLoginClient struct {
	status QRStatusResponse
	block  chan struct{}
}

func (f *fakeLoginClient) StartLogin(context.Context, []string) (QRCodeResponse, error) {
	return QRCodeResponse{QRCode: "qr-id", QRCodeImageURL: "data:image/png;base64,qr"}, nil
}

func (f *fakeLoginClient) PollLogin(ctx context.Context, _, _, _ string) (QRStatusResponse, error) {
	if f.block != nil {
		select {
		case <-ctx.Done():
			return QRStatusResponse{}, ctx.Err()
		case <-f.block:
		}
	}
	return f.status, nil
}

type fakeCredentialStore struct {
	mu     sync.Mutex
	saved  LoginRequest
	status QRStatusResponse
}

func (*fakeCredentialStore) LocalTokens() []string { return []string{"existing"} }
func (s *fakeCredentialStore) SaveWechatCredential(_ context.Context, req LoginRequest, status QRStatusResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved, s.status = req, status
	return nil
}

func TestLoginManagerPersistsConfirmedCredential(t *testing.T) {
	store := &fakeCredentialStore{}
	manager := NewLoginManager(&fakeLoginClient{status: QRStatusResponse{Status: "confirmed", BotToken: "secret", BotID: "bot-id"}}, store)
	defer manager.Close()
	snapshot, err := manager.Start(context.Background(), LoginRequest{AccountID: "personal", Name: "Personal", BindType: "l1"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = manager.Snapshot(snapshot.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Status == LoginConnected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snapshot.Status != LoginConnected || snapshot.QRPayload != "" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.saved.AccountID != "personal" || store.status.BotToken != "secret" {
		t.Fatalf("saved = %#v status=%#v", store.saved, store.status)
	}
}

func TestLoginManagerRejectsConcurrentLoginForAccount(t *testing.T) {
	manager := NewLoginManager(&fakeLoginClient{block: make(chan struct{})}, nil)
	defer manager.Close()
	request := LoginRequest{AccountID: "personal", BindType: "l1"}
	if _, err := manager.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(context.Background(), request); err != ErrLoginConflict {
		t.Fatalf("second start error = %v", err)
	}
}
