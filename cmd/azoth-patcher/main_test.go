//go:build windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KyzaBOY/azoth-launcher/internal/client"
	"github.com/KyzaBOY/azoth-launcher/internal/patcher"
)

const testTicket = "ticket-must-stay-secret"

// pipes makes a request file with content and an empty reply file. Plain
// files stand in for the FIFOs: the helper only opens, reads and writes them.
func pipes(t *testing.T, request string) (req, rep string) {
	t.Helper()
	dir := t.TempDir()
	req, rep = filepath.Join(dir, "req"), filepath.Join(dir, "rep")
	if err := os.WriteFile(req, []byte(request), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rep, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return req, rep
}

func replies(t *testing.T, path string) []patcher.Reply {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var out []patcher.Reply
	for {
		rep, err := patcher.ReadReply(r)
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, rep)
	}
}

func goodRequest() string {
	return fmt.Sprintf(`{"v":1,"binDir":"C:\\copy\\Bin64","gatewayAddr":"play.example.test:6000","pingHost":"play.example.test","persona":%q}`, testTicket) + "\n"
}

func TestRunLaunches(t *testing.T) {
	req, rep := pipes(t, goodRequest())
	var got client.LaunchOptions
	launch := func(o client.LaunchOptions) (*client.LaunchResult, error) {
		got = o
		return &client.LaunchResult{PID: 7, PersonaPatched: true}, nil
	}
	if code := run([]string{"-request", req, "-reply", rep}, io.Discard, launch); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	want := client.LaunchOptions{BinDir: `C:\copy\Bin64`, GatewayAddr: "play.example.test:6000", PingHost: "play.example.test", Persona: testTicket}
	if got.BinDir != want.BinDir || got.GatewayAddr != want.GatewayAddr || got.PingHost != want.PingHost || got.Persona != want.Persona || got.Log != nil {
		t.Fatalf("launch got %+v", got)
	}
	reps := replies(t, rep)
	if len(reps) != 2 || reps[0].Stage != patcher.StageReady || reps[1].Stage != patcher.StageDone || !reps[1].OK || reps[1].Result.PID != 7 {
		t.Fatalf("replies = %+v", reps)
	}
	raw, _ := os.ReadFile(rep)
	if strings.Contains(string(raw), testTicket) {
		t.Fatal("the ticket must never be in a reply")
	}
}

func TestRunPatchFailed(t *testing.T) {
	req, rep := pipes(t, goodRequest())
	launch := func(client.LaunchOptions) (*client.LaunchResult, error) {
		return &client.LaunchResult{PID: 9}, fmt.Errorf("%w: [dtls] unexpected bytes", client.ErrPatchFailed)
	}
	if code := run([]string{"-request", req, "-reply", rep}, io.Discard, launch); code != exitFailed {
		t.Fatalf("exit %d", code)
	}
	reps := replies(t, rep)
	if len(reps) != 2 || reps[1].OK || !reps[1].PatchFailed || !strings.Contains(reps[1].Error, "dtls") {
		t.Fatalf("replies = %+v", reps)
	}
}

func TestRunRefusesWithoutHandshake(t *testing.T) {
	noLaunch := func(client.LaunchOptions) (*client.LaunchResult, error) {
		t.Fatal("launch must not run")
		return nil, nil
	}
	req, rep := pipes(t, goodRequest())
	missing := filepath.Join(t.TempDir(), "nope")
	for name, args := range map[string][]string{
		"no flags":        nil,
		"only request":    {"-request", req},
		"only reply":      {"-reply", rep},
		"extra argument":  {"-request", req, "-reply", rep, "extra"},
		"unknown flag":    {"-request", req, "-reply", rep, "-persona", testTicket},
		"no reply pipe":   {"-request", req, "-reply", missing},
		"no request pipe": {"-request", missing, "-reply", rep},
	} {
		var stderr strings.Builder
		if code := run(args, &stderr, noLaunch); code != exitRefused {
			t.Errorf("%s: exit %d", name, code)
		}
		if !strings.Contains(stderr.String(), usageRefusal) {
			t.Errorf("%s: stderr = %q", name, stderr.String())
		}
	}

	for name, body := range map[string]string{
		"wrong version": `{"v":2,"binDir":"C:\\b","gatewayAddr":"h:1"}` + "\n",
		"garbage":       "hello\n",
		"empty":         "",
	} {
		req, rep := pipes(t, body)
		if code := run([]string{"-request", req, "-reply", rep}, io.Discard, noLaunch); code != exitRefused {
			t.Errorf("%s: exit %d", name, code)
		}
	}
}
