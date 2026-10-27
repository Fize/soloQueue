package wechat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetConfigReturnsOptionalContextToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ret":0,"typing_ticket":"ticket","context_token":"renewed"}`)
	}))
	defer server.Close()
	client := NewClient(Config{BaseURL: server.URL, Token: "fake"})
	got, err := client.GetConfig(context.Background(), "recipient", "old")
	if err != nil {
		t.Fatal(err)
	}
	if got.ContextToken != "renewed" {
		t.Fatal("returned context token was discarded")
	}
}
