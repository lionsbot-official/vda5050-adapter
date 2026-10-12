package lionsbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginTokenField  = "token"
	loginExpiryField = "tokenExpiryIsoUtcTime"
)

// Credentials are the login inputs. Password is never logged or returned in errors.
type Credentials struct {
	Email      string
	Password   string
	AppName    string
	AppVersion string
	Origin     string
	Headers    map[string]string
}

// TokenProvider logs in for an access token and its expiry, and holds the latest.
type TokenProvider struct {
	url    string
	creds  Credentials
	client *http.Client

	mu     sync.RWMutex
	token  string
	expiry time.Time
}

func NewTokenProvider(url string, creds Credentials, client *http.Client) *TokenProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &TokenProvider{url: url, creds: creds, client: client}
}

// Login POSTs the credentials, stores and returns the token and its expiry.
func (p *TokenProvider) Login(ctx context.Context) (string, time.Time, error) {
	return p.login(ctx, nil)
}

// LoginDebug logs the request and response to w with the password redacted.
func (p *TokenProvider) LoginDebug(ctx context.Context, w io.Writer) (string, time.Time, error) {
	return p.login(ctx, w)
}

// Token returns the token and expiry from the last successful Login.
func (p *TokenProvider) Token() (string, time.Time) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.token, p.expiry
}

func (p *TokenProvider) login(ctx context.Context, dbg io.Writer) (string, time.Time, error) {
	req, payload, err := p.buildRequest(ctx)
	if err != nil {
		return "", time.Time{}, err
	}

	if dbg != nil {
		fmt.Fprintf(dbg, "POST %s\n", p.url)
		for k, v := range req.Header {
			fmt.Fprintf(dbg, "  %s: %s\n", k, strings.Join(v, ","))
		}
		fmt.Fprintf(dbg, "  body: %s\n", redactPassword(payload))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if dbg != nil {
		fmt.Fprintf(dbg, "-> HTTP %d\n  body: %s\n", resp.StatusCode, snippet(body, 1200))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("login HTTP %d: %s", resp.StatusCode, snippet(body, 300))
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", time.Time{}, fmt.Errorf("login response not JSON: %w", err)
	}

	var token string
	if err := json.Unmarshal(raw[loginTokenField], &token); err != nil || token == "" {
		return "", time.Time{}, fmt.Errorf("login response missing %q field", loginTokenField)
	}

	var expiry time.Time
	if rawExp, ok := raw[loginExpiryField]; ok {
		var s string
		if json.Unmarshal(rawExp, &s) == nil && s != "" {
			if t, perr := time.Parse(time.RFC3339, s); perr == nil {
				expiry = t
			}
		}
	}

	p.mu.Lock()
	p.token, p.expiry = token, expiry
	p.mu.Unlock()
	return token, expiry, nil
}

func (p *TokenProvider) buildRequest(ctx context.Context) (*http.Request, []byte, error) {
	body := map[string]string{"email": p.creds.Email, "password": p.creds.Password}
	if p.creds.AppName != "" {
		body["applicationName"] = p.creds.AppName
	}
	if p.creds.AppVersion != "" {
		body["applicationVersion"] = p.creds.AppVersion
	}
	payload, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(payload))
	if err != nil {
		return nil, payload, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if p.creds.Origin != "" {
		req.Header.Set("Origin", p.creds.Origin)
	}
	for k, v := range p.creds.Headers {
		req.Header.Set(k, v)
	}
	return req, payload, nil
}

func snippet(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func redactPassword(payload []byte) string {
	var m map[string]string
	if json.Unmarshal(payload, &m) != nil {
		return "<unparseable>"
	}
	if _, ok := m["password"]; ok {
		m["password"] = "***"
	}
	out, _ := json.Marshal(m)
	return string(out)
}
