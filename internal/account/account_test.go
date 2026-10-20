package account

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var b64url43 = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// fakeCentral plays the account server: /login redirects to the launcher's
// loopback (like the real callback), /token checks the PKCE, /ticket checks the session.
type fakeCentral struct {
	srv *httptest.Server

	callback  string // what /login answers in the redirect: "code=..." or "error=..."
	tokenGate chan struct{}
	tokenIn   chan struct{} // gets a signal when /token is reached (before the gate)
	tokenSt   int           // forced status of /token (0 = normal behaviour)
	ticketSt  int           // status of /ticket (0 = 200)

	mu        sync.Mutex
	challenge string
	port      string
	verified  bool
	tokenCall int
	ticketHdr string
	ticketLen int64
}

func newFakeCentral(t *testing.T) *fakeCentral {
	t.Helper()
	f := &fakeCentral{callback: "code=abc123"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/launcher/login", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.port, f.challenge = r.URL.Query().Get("port"), r.URL.Query().Get("challenge")
		cb := f.callback
		f.mu.Unlock()
		http.Redirect(w, r, "http://127.0.0.1:"+f.port+"/callback?"+cb, http.StatusSeeOther)
	})
	mux.HandleFunc("POST /auth/launcher/token", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Code, Verifier string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if f.tokenIn != nil {
			f.tokenIn <- struct{}{}
		}
		if f.tokenGate != nil {
			<-f.tokenGate
		}
		sum := sha256.Sum256([]byte(in.Verifier))
		f.mu.Lock()
		f.tokenCall++
		ok := in.Code == "abc123" && b64url43.MatchString(in.Verifier) &&
			base64.RawURLEncoding.EncodeToString(sum[:]) == f.challenge
		f.verified = ok
		forced := f.tokenSt
		f.mu.Unlock()
		if forced != 0 {
			w.WriteHeader(forced)
			io.WriteString(w, `{"error":"not allowed"}`)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"invalid code"}`)
			return
		}
		io.WriteString(w, `{"session":"sess-token","expires":4102444800,"user":{"id":"123456789012345678","name":"alice"}}`)
	})
	mux.HandleFunc("POST /auth/launcher/ticket", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.ticketHdr, f.ticketLen = r.Header.Get("Authorization"), r.ContentLength
		st := f.ticketSt
		f.mu.Unlock()
		if st == 0 && f.ticketHdr != "Bearer sess-token" {
			st = http.StatusUnauthorized
		}
		if st != 0 {
			w.WriteHeader(st)
			io.WriteString(w, `{"error":"x"}`)
			return
		}
		io.WriteString(w, `{"ticket":"az1-ticket","expires":4102444800,"user":{"id":"123456789012345678","name":"alice"}}`)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// browser imitates the browser: it follows the account server's /login to the launcher's loopback.
func browser(done chan<- *http.Response) func(string) error {
	return func(u string) error {
		go func() {
			resp, err := http.Get(u)
			if err != nil {
				done <- nil
				return
			}
			done <- resp
		}()
		return nil
	}
}

func TestLoginOK(t *testing.T) {
	f := newFakeCentral(t)
	var loginURL string
	pageCh := make(chan *http.Response, 1)
	b := browser(pageCh)
	s, err := Login(context.Background(), f.srv.URL+"/", func(u string) error { loginURL = u; return b(u) })
	if err != nil {
		t.Fatal(err)
	}
	if s.Token != "sess-token" || s.Expires != 4102444800 || s.User.ID != "123456789012345678" || s.User.Name != "alice" {
		t.Fatalf("session = %+v", s)
	}

	u, err := url.Parse(loginURL)
	if err != nil || u.Path != "/auth/launcher/login" || !strings.HasPrefix(loginURL, f.srv.URL+"/auth/") {
		t.Fatalf("login url = %q (%v)", loginURL, err)
	}
	port, _ := strconv.Atoi(u.Query().Get("port"))
	if port < 1024 || port > 65535 || !b64url43.MatchString(u.Query().Get("challenge")) {
		t.Fatalf("invalid port/challenge: %q", loginURL)
	}
	f.mu.Lock()
	verified := f.verified
	f.mu.Unlock()
	if !verified {
		t.Fatal("/token did not check sha256(verifier) == challenge")
	}

	resp := <-pageCh
	if resp == nil {
		t.Fatal("the browser got no page")
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "re logged in. You can close this tab") || strings.Contains(string(body), "abc123") {
		t.Fatalf("page = %d %s", resp.StatusCode, body)
	}
	// the listener is closed when it ends
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 500*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("the loopback listener is still open")
	}
}

func TestLoginErrors(t *testing.T) {
	for _, tc := range []struct {
		cb   string
		want error
		page string
	}{
		{"error=not_member", ErrNotMember, "not in the Azoth server"},
		{"error=denied", ErrDenied, "Login cancelled."},
		{"error=failed", nil, "Login failed. Try again."},
		{"error=state", nil, "Login failed. Try again."},
		{"error=%3Cscript%3E", nil, "Login failed. Try again."},
	} {
		f := newFakeCentral(t)
		f.callback = tc.cb
		pageCh := make(chan *http.Response, 1)
		_, err := Login(context.Background(), f.srv.URL, browser(pageCh))
		if err == nil {
			t.Fatalf("%s: must fail", tc.cb)
		}
		if tc.want != nil && !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", tc.cb, err, tc.want)
		}
		if tc.want == nil && (errors.Is(err, ErrNotMember) || errors.Is(err, ErrDenied)) {
			t.Fatalf("%s: expected a generic error, got %v", tc.cb, err)
		}
		if strings.Contains(err.Error(), "<") {
			t.Fatalf("%s: the error reflects the browser's value: %v", tc.cb, err)
		}
		resp := <-pageCh
		if resp == nil {
			t.Fatalf("%s: the browser got no page", tc.cb)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(body), tc.page) || strings.Contains(string(body), "script") {
			t.Fatalf("%s: page = %d %s", tc.cb, resp.StatusCode, body)
		}
		f.mu.Lock()
		calls := f.tokenCall
		f.mu.Unlock()
		if calls != 0 {
			t.Fatalf("%s: must not call /token", tc.cb)
		}
	}
}

func TestLoginTokenRejected(t *testing.T) {
	f := newFakeCentral(t)
	f.callback = "code=other" // the account server does not know this code
	pageCh := make(chan *http.Response, 1)
	_, err := Login(context.Background(), f.srv.URL, browser(pageCh))
	if err == nil || errors.Is(err, ErrNotMember) {
		t.Fatalf("err = %v", err)
	}
	resp := <-pageCh
	if resp == nil {
		t.Fatal("the browser got no page")
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Login failed") || strings.Contains(string(body), "logged in") {
		t.Fatalf("the tab must not say it logged in: %s", body)
	}
}

func TestLoginNotAllowed(t *testing.T) {
	f := newFakeCentral(t)
	f.tokenSt = http.StatusForbidden
	pageCh := make(chan *http.Response, 1)
	_, err := Login(context.Background(), f.srv.URL, browser(pageCh))
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("403 on /token = %v", err)
	}
	resp := <-pageCh
	if resp == nil {
		t.Fatal("the browser got no page")
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "doesn&#39;t have access yet") || strings.Contains(string(body), "logged in") {
		t.Fatalf("page = %d %s", resp.StatusCode, body)
	}
}

func TestLoginOnlyFirstCallback(t *testing.T) {
	f := newFakeCentral(t)
	f.tokenGate = make(chan struct{}) // holds the first code's exchange while the second callback arrives
	f.tokenIn = make(chan struct{}, 1)
	portCh := make(chan string, 1)
	pageCh := make(chan *http.Response, 1)
	res := make(chan error, 1)
	b := browser(pageCh)
	go func() {
		_, err := Login(context.Background(), f.srv.URL, func(u string) error {
			pu, _ := url.Parse(u)
			portCh <- pu.Query().Get("port")
			return b(u) // first callback: through the account server's /login, like a real browser
		})
		res <- err
	}()
	port := <-portCh
	select {
	case <-f.tokenIn: // the first callback already took the turn and is at /token
	case <-time.After(5 * time.Second):
		t.Fatal("the first callback did not reach /token")
	}

	resp, err := http.Get("http://127.0.0.1:" + port + "/callback?code=xyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("second callback = %d, want 410", resp.StatusCode)
	}
	if resp, err := http.Post("http://127.0.0.1:"+port+"/callback?code=x", "text/plain", nil); err != nil {
		t.Fatal(err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("POST /callback = %d", resp.StatusCode)
		}
	}

	close(f.tokenGate)
	if err := <-res; err != nil {
		t.Fatal(err)
	}
	if resp := <-pageCh; resp == nil || resp.StatusCode != 200 {
		t.Fatalf("the first call had to log in: %+v", resp)
	}
}

func TestLoginEmptyCallbackDoesNotBurnTheTurn(t *testing.T) {
	f := newFakeCentral(t)
	pageCh := make(chan *http.Response, 1)
	b := browser(pageCh)
	s, err := Login(context.Background(), f.srv.URL, func(u string) error {
		pu, _ := url.Parse(u)
		resp, err := http.Get("http://127.0.0.1:" + pu.Query().Get("port") + "/callback")
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("empty callback = %d", resp.StatusCode)
		}
		resp, err = http.Get("http://127.0.0.1:" + pu.Query().Get("port") + "/other")
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("other path = %d", resp.StatusCode)
		}
		return b(u)
	})
	if err != nil || s.Token == "" {
		t.Fatalf("login = %+v %v", s, err)
	}
	<-pageCh
}

func TestLoginTimeoutAndOpenError(t *testing.T) {
	f := newFakeCentral(t)
	var port string
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := Login(ctx, f.srv.URL, func(u string) error {
		pu, _ := url.Parse(u)
		port = pu.Query().Get("port")
		return nil // the browser never comes back
	})
	if err == nil {
		t.Fatal("must time out")
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 500*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("the listener stayed open after the deadline")
	}

	boom := errors.New("no browser")
	if _, err := Login(context.Background(), f.srv.URL, func(string) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestTicket(t *testing.T) {
	f := newFakeCentral(t)
	ctx := context.Background()

	tk, exp, err := Ticket(ctx, f.srv.URL+"/", "sess-token")
	if err != nil || tk != "az1-ticket" || exp != 4102444800 {
		t.Fatalf("ticket = %q %d %v", tk, exp, err)
	}
	f.mu.Lock()
	hdr, clen := f.ticketHdr, f.ticketLen
	f.mu.Unlock()
	if hdr != "Bearer sess-token" || clen > 0 {
		t.Fatalf("header = %q, content-length = %d", hdr, clen)
	}

	if _, _, err := Ticket(ctx, f.srv.URL, "other"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("401 = %v", err)
	}
	f.mu.Lock()
	f.ticketSt = http.StatusServiceUnavailable
	f.mu.Unlock()
	if _, _, err := Ticket(ctx, f.srv.URL, "sess-token"); err == nil || !strings.Contains(err.Error(), "login is off on the server") || errors.Is(err, ErrSessionExpired) {
		t.Fatalf("503 = %v", err)
	}
	f.mu.Lock()
	f.ticketSt = http.StatusForbidden
	f.mu.Unlock()
	if _, _, err := Ticket(ctx, f.srv.URL, "sess-token"); !errors.Is(err, ErrNotAllowed) || errors.Is(err, ErrSessionExpired) || strings.Contains(err.Error(), "sess-token") {
		t.Fatalf("403 = %v", err)
	}
	f.mu.Lock()
	f.ticketSt = http.StatusInternalServerError
	f.mu.Unlock()
	if _, _, err := Ticket(ctx, f.srv.URL, "sess-token"); err == nil || !strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "sess-token") {
		t.Fatalf("500 = %v", err)
	}

	// server down: a network error, without the token in the text
	f.srv.Close()
	if _, _, err := Ticket(ctx, f.srv.URL, "sess-token"); err == nil || errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrNotAllowed) || strings.Contains(err.Error(), "sess-token") {
		t.Fatalf("network = %v", err)
	}
}

func TestValidateURL(t *testing.T) {
	for _, ok := range []string{"https://projectazoth.com", "https://projectazoth.com/", " https://projectazoth.com ", "http://127.0.0.1:8090", "http://localhost:8090/", "https://account.test/azoth"} {
		if err := ValidateURL(ok); err != nil {
			t.Fatalf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "projectazoth.com", "http://projectazoth.com", "http://127.0.0.1.evil.com", "http://localhost.evil.com", "ftp://projectazoth.com", "https://", "https://u:p@projectazoth.com", "https://projectazoth.com/?x=1", "javascript:alert(1)"} {
		if err := ValidateURL(bad); err == nil {
			t.Fatalf("%q must fail", bad)
		}
	}
}

func TestStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "AzothLauncher") // Save creates the directory
	if _, ok := Load(dir); ok {
		t.Fatal("no file means no session")
	}
	want := Session{Token: "secret-token-123", Expires: time.Now().Add(time.Hour).Unix(), User: User{ID: "42", Name: "alice"}}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secret-token-123")) || bytes.Contains(raw, []byte("alice")) {
		t.Fatal("account.dat is in plain text")
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".tmp")); err == nil {
		t.Fatal("the .tmp file was left behind")
	}
	got, ok := Load(dir)
	if !ok || got != want {
		t.Fatalf("round trip = %+v %v", got, ok)
	}

	// overwrite (logging in again)
	want2 := Session{Token: "other", Expires: want.Expires + 10, User: User{ID: "43", Name: "bob"}}
	if err := Save(dir, want2); err != nil {
		t.Fatal(err)
	}
	if got, ok := Load(dir); !ok || got != want2 {
		t.Fatalf("after overwriting = %+v %v", got, ok)
	}

	// expired
	if err := Save(dir, Session{Token: "old", Expires: time.Now().Add(-time.Minute).Unix(), User: User{ID: "42"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load(dir); ok {
		t.Fatal("an expired session must not load")
	}

	// corrupted
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("this is not dpapi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load(dir); ok {
		t.Fatal("a corrupted file must not load")
	}

	// Clear: deletes, and does not complain about a missing file
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	if err := Clear(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load(dir); ok {
		t.Fatal("no session after Clear")
	}
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear without a file: %v", err)
	}
}
