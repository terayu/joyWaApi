package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler/transport"
)

func TestIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": true,
		"localhost:8080": true,
		"[::1]:8080":     true,
		"0.0.0.0:8080":   false,
		":8080":          false,
		"192.168.1.10:1": false,
		"bad":            false,
	} {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

// TestNewServerRequiresToken ループバック以外で待ち受けるときはトークンが必要
func TestNewServerRequiresToken(t *testing.T) {
	for _, c := range []struct {
		cfg ServerConfig
		ok  bool
	}{
		{ServerConfig{Addr: "127.0.0.1:0"}, true},
		{ServerConfig{Addr: "0.0.0.0:0"}, false},
		{ServerConfig{Addr: "0.0.0.0:0", Token: "x"}, true},
		{ServerConfig{Addr: "0.0.0.0:0", Insecure: true}, true},
	} {
		if _, err := NewServer(nil, c.cfg); (err == nil) != c.ok {
			t.Errorf("%+v: err = %v", c.cfg, err)
		}
	}
}

func TestAuth(t *testing.T) {
	s := &Server{cfg: ServerConfig{Token: "secret"}}
	h := s.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	tests := []struct {
		name   string
		header map[string]string
		want   int
	}{
		{"no token", nil, http.StatusUnauthorized},
		{"wrong token", map[string]string{"Authorization": "Bearer wrong"}, http.StatusUnauthorized},
		{"ok", map[string]string{"Authorization": "Bearer secret"}, http.StatusOK},
		{"websocket without header", map[string]string{"Upgrade": "websocket"}, http.StatusOK},
		{"websocket wrong token", map[string]string{"Upgrade": "websocket", "Authorization": "Bearer wrong"}, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodPost, "/query", nil)
		for k, v := range tt.header {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Errorf("%s: status = %v, want %v", tt.name, w.Code, tt.want)
		}
	}

	// トークンを設定しなければ確認しない
	w := httptest.NewRecorder()
	(&Server{}).auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/query", nil))
	if w.Code != http.StatusOK {
		t.Errorf("no token configured: status = %v", w.Code)
	}
}

func TestWsInit(t *testing.T) {
	s := &Server{cfg: ServerConfig{Token: "secret"}}
	ctx := context.Background()
	if _, _, err := s.wsInit(ctx, transport.InitPayload{"Authorization": "Bearer secret"}); err != nil {
		t.Errorf("valid payload: %v", err)
	}
	if _, _, err := s.wsInit(ctx, transport.InitPayload{"Authorization": "Bearer wrong"}); err == nil {
		t.Error("wrong payload: no error")
	}
	if _, _, err := s.wsInit(ctx, transport.InitPayload{}); err == nil {
		t.Error("empty payload: no error")
	}
	if _, _, err := s.wsInit(context.WithValue(ctx, authorizedKey{}, true), transport.InitPayload{}); err != nil {
		t.Errorf("authorized by header: %v", err)
	}
}
