// Package account is the launcher's Discord login. It is the native-app
// pattern of RFC 8252 with PKCE: the launcher listens on 127.0.0.1:<port>,
// opens the browser on the account site, and the browser comes back to the
// loopback with a single-use code that is only worth something together with
// the verifier. The code becomes a session (30 days, stored with DPAPI) and on
// every Play the session becomes a 24 h ticket, which is the client's persona.
// The session and the ticket never go to a log.
package account

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// LoginTimeout is the total time the browser has to come back to the loopback.
	LoginTimeout = 5 * time.Minute

	httpTimeout  = 15 * time.Second
	maxBody      = 64 << 10 // an answer from the account server never gets near this
	shutdownWait = 2 * time.Second
)

var (
	// ErrNotMember: Discord authenticated the person, but they are not in the Azoth server.
	ErrNotMember = errors.New("your Discord account is not in the Azoth server; join it and try again")
	// ErrDenied: the person declined (or closed) Discord's authorization screen.
	ErrDenied = errors.New("login cancelled")
	// ErrSessionExpired: the account server no longer recognizes the session (it expired or the key changed).
	ErrSessionExpired = errors.New("login expired")
	// ErrNotAllowed: the account is logged in but is not on the server's access list (HTTP 403).
	// The session is still valid, so callers must not clear it.
	ErrNotAllowed = errors.New("this Discord account is not on the access list")

	errLoginOff = errors.New("account login is off on the server")
)

// User is who is logged in.
type User struct {
	ID   string `json:"id"` // Discord id
	Name string `json:"name"`
}

// Session is what /auth/launcher/token returns.
type Session struct {
	Token   string `json:"session"`
	Expires int64  `json:"expires"` // Unix, segundos
	User    User   `json:"user"`
}

// ValidateURL checks the account server address. The session travels to it, so
// it must be https; plain http is only accepted on the loopback, to test
// against a local server.
func ValidateURL(v string) error {
	v = strings.TrimRight(strings.TrimSpace(v), "/")
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid account server address: %q", v)
	}
	loopback := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"
	if u.Scheme == "https" || (u.Scheme == "http" && loopback) {
		return nil
	}
	return fmt.Errorf("invalid account server address: %q (use https://, or http://127.0.0.1 / http://localhost for local tests)", v)
}

// httpClient does not follow redirects: the account server answers directly,
// and a 3xx would be a misconfiguration (the session must not end up on another host).
var httpClient = &http.Client{
	Timeout:       httpTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Login does the whole login: it opens the browser (through open) on the
// account site, waits for the callback on the loopback and exchanges the code
// for the session. It ends with ctx, at LoginTimeout or on the first callback;
// the listener is closed in every case.
func Login(ctx context.Context, baseURL string, open func(url string) error) (Session, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	ctx, cancel := context.WithTimeout(ctx, LoginTimeout)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Session{}, fmt.Errorf("could not listen on the loopback: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if port < 1024 { // the account site only accepts 1024..65535
		_ = ln.Close()
		return Session{}, fmt.Errorf("unusable loopback port: %d", port)
	}

	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		_ = ln.Close()
		return Session{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw[:]) // 43 characters
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	type outcome struct {
		s   Session
		err error
	}
	done := make(chan outcome, 1)
	var claimed atomic.Bool

	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code, errCode := q.Get("code"), q.Get("error")
		if code == "" && errCode == "" { // not the account site's redirect: do not spend the only turn
			writePage(w, http.StatusBadRequest, "Invalid login callback.")
			return
		}
		if !claimed.CompareAndSwap(false, true) {
			writePage(w, http.StatusGone, "This login link was already used. Go back to the Azoth launcher.")
			return
		}
		// The code exchange happens before the answer: the tab only says "logged in" if it really did.
		var out outcome
		if errCode != "" {
			out.err = loginError(errCode)
		} else {
			out.s, out.err = exchange(ctx, baseURL, code, verifier)
		}
		switch {
		case out.err == nil:
			writePage(w, http.StatusOK, "You're logged in. You can close this tab and go back to the Azoth launcher.")
		case errors.Is(out.err, ErrNotMember):
			writePage(w, http.StatusForbidden, "Your Discord account is not in the Azoth server. Join it and try again.")
		case errors.Is(out.err, ErrNotAllowed):
			writePage(w, http.StatusForbidden, "Your Discord account doesn't have access yet. The playtest is closed for now.")
		case errors.Is(out.err, ErrDenied):
			writePage(w, http.StatusBadRequest, "Login cancelled.")
		default:
			writePage(w, http.StatusBadRequest, "Login failed. Try again.")
		}
		done <- out
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		sctx, scancel := context.WithTimeout(context.Background(), shutdownWait)
		defer scancel()
		if srv.Shutdown(sctx) != nil {
			_ = srv.Close()
		}
	}()

	loginURL := baseURL + "/auth/launcher/login?port=" + strconv.Itoa(port) + "&challenge=" + challenge
	if err := open(loginURL); err != nil {
		return Session{}, fmt.Errorf("could not open the browser: %w", err)
	}

	select {
	case out := <-done:
		return out.s, out.err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Session{}, errors.New("login timed out: the browser did not come back in time")
		}
		return Session{}, ctx.Err()
	}
}

// loginError translates the callback's ?error=. The value comes from the
// browser: only short lowercase/underscore text makes it into the error message.
func loginError(code string) error {
	switch code {
	case "not_member":
		return ErrNotMember
	case "denied":
		return ErrDenied
	}
	for _, r := range code {
		if (r < 'a' || r > 'z') && r != '_' {
			return errors.New("login failed")
		}
	}
	if len(code) > 24 {
		return errors.New("login failed")
	}
	return fmt.Errorf("login failed (%s)", code)
}

// exchange trades code + verifier for the session (POST /auth/launcher/token).
func exchange(ctx context.Context, baseURL, code, verifier string) (Session, error) {
	body, _ := json.Marshal(map[string]string{"code": code, "verifier": verifier})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/auth/launcher/token", bytes.NewReader(body))
	if err != nil {
		return Session{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Session{}, fmt.Errorf("could not reach the account server: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return Session{}, errors.New("the account server rejected the login code")
	case http.StatusForbidden:
		return Session{}, ErrNotAllowed
	case http.StatusServiceUnavailable:
		return Session{}, errLoginOff
	default:
		return Session{}, fmt.Errorf("account server answered %d", resp.StatusCode)
	}
	var s Session
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&s); err != nil {
		return Session{}, fmt.Errorf("invalid answer from the account server: %w", err)
	}
	if s.Token == "" || s.Expires <= 0 || s.User.ID == "" {
		return Session{}, errors.New("incomplete answer from the account server")
	}
	return s, nil
}

// Ticket trades the session for a 24 h ticket (POST /auth/launcher/ticket),
// which is what goes in as the client's persona. 401 = session expired,
// 403 = the account is not on the access list.
func Ticket(ctx context.Context, baseURL, token string) (ticket string, expires int64, err error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/auth/launcher/ticket", nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return "", 0, ErrSessionExpired
	case http.StatusForbidden:
		return "", 0, ErrNotAllowed
	case http.StatusServiceUnavailable:
		return "", 0, errLoginOff
	default:
		return "", 0, fmt.Errorf("account server answered %d", resp.StatusCode)
	}
	var out struct {
		Ticket  string `json:"ticket"`
		Expires int64  `json:"expires"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&out); err != nil {
		return "", 0, fmt.Errorf("invalid answer from the account server: %w", err)
	}
	if out.Ticket == "" {
		return "", 0, errors.New("incomplete answer from the account server")
	}
	return out.Ticket, out.Expires, nil
}

// writePage answers the browser tab. The text is always a constant from here;
// nothing that came in the URL is reflected.
func writePage(w http.ResponseWriter, status int, msg string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	fmt.Fprintf(w, pageHTML, html.EscapeString(msg))
}

const pageHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Azoth</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#080606;color:#ece1cc;font:19px/1.45 Georgia,serif;text-align:center}
main{max-width:30em;padding:2em}
h1{margin:0 0 .6em;font-weight:400;font-size:34px;letter-spacing:.3em;text-transform:uppercase;color:#cfae6a}
p{margin:0}
</style></head>
<body><main><h1>Azoth</h1><p>%s</p></main></body></html>
`
