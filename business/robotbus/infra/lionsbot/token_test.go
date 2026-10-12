package lionsbot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTokenProvider_LoginParsesTokenAndExpiry(t *testing.T) {
	const wantToken = "access-jwt-abc"
	const wantExpiryStr = "2026-07-16T10:30:00Z"

	var gotMethod, gotContentType string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + wantToken + `","tokenExpiryIsoUtcTime":"` + wantExpiryStr + `"}`))
	}))
	defer srv.Close()

	p := NewTokenProvider(srv.URL, Credentials{
		Email:      "robot@example.com",
		Password:   "s3cret",
		AppName:    "adapter",
		AppVersion: "1.2.3",
	}, srv.Client())

	token, expiry, err := p.Login(context.Background())
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if token != wantToken {
		t.Errorf("token = %q, want %q", token, wantToken)
	}
	wantExpiry, _ := time.Parse(time.RFC3339, wantExpiryStr)
	if !expiry.Equal(wantExpiry) {
		t.Errorf("expiry = %v, want %v", expiry, wantExpiry)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Errorf("content-type = %q, want application/json", gotContentType)
	}

	wantBody := map[string]any{
		"email":              "robot@example.com",
		"password":           "s3cret",
		"applicationName":    "adapter",
		"applicationVersion": "1.2.3",
	}
	if len(gotBody) != len(wantBody) {
		t.Fatalf("body = %v, want %v", gotBody, wantBody)
	}
	for k, v := range wantBody {
		if gotBody[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, gotBody[k], v)
		}
	}

	if st, se := p.Token(); st != wantToken || !se.Equal(wantExpiry) {
		t.Errorf("Token() = (%q,%v), want (%q,%v)", st, se, wantToken, wantExpiry)
	}
}

func TestTokenProvider_LoginOmitsEmptyAppFields(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"t","tokenExpiryIsoUtcTime":"2026-07-16T10:30:00Z"}`))
	}))
	defer srv.Close()

	p := NewTokenProvider(srv.URL, Credentials{Email: "e@x.io", Password: "s3cret"}, srv.Client())
	if _, _, err := p.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, ok := gotBody["applicationName"]; ok {
		t.Errorf("applicationName present, want omitted: %v", gotBody)
	}
	if _, ok := gotBody["applicationVersion"]; ok {
		t.Errorf("applicationVersion present, want omitted: %v", gotBody)
	}
}

func TestTokenProvider_LoginErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"non-2xx", http.StatusUnauthorized, `{"message":"nope"}`, "401"},
		{"malformed json", http.StatusOK, `not json`, "not JSON"},
		{"missing token", http.StatusOK, `{"tokenExpiryIsoUtcTime":"2026-07-16T10:30:00Z"}`, "token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			p := NewTokenProvider(srv.URL, Credentials{Email: "e@x.io", Password: "s3cret"}, srv.Client())
			_, _, err := p.Login(context.Background())
			if err == nil {
				t.Fatal("Login err = nil, want error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want containing %q", err.Error(), tc.wantErr)
			}
			if strings.Contains(err.Error(), "s3cret") {
				t.Errorf("error leaked password: %q", err.Error())
			}
		})
	}
}

func TestTokenProvider_LoginDebugRedactsPassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"t","tokenExpiryIsoUtcTime":"2026-07-16T10:30:00Z"}`))
	}))
	defer srv.Close()

	var dbg bytes.Buffer
	p := NewTokenProvider(srv.URL, Credentials{Email: "e@x.io", Password: "s3cret"}, srv.Client())
	if _, _, err := p.LoginDebug(context.Background(), &dbg); err != nil {
		t.Fatalf("LoginDebug: %v", err)
	}
	out := dbg.String()
	if strings.Contains(out, "s3cret") {
		t.Errorf("debug output leaked password: %s", out)
	}
	if !strings.Contains(out, "***") {
		t.Errorf("debug output missing redaction marker: %s", out)
	}
}
