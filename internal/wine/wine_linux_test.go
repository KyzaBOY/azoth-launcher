package wine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KyzaBOY/azoth-launcher/internal/client"
	"github.com/KyzaBOY/azoth-launcher/internal/patcher"
)

const testTicket = "ticket-must-stay-secret"

// fakeUmu stands in for umu-run + Proton + the helper: it answers on the
// pipes as azoth-patcher.exe does and records what it was given in $LOG.*.
const fakeUmu = `#!/bin/sh
unix() { printf '%s' "$1" | sed 's#^Z:##; s#\\#/#g'; }
req=$(unix "$3"); rep=$(unix "$5")
printf '%s\n' "$*" > "$LOG.args"
env > "$LOG.env"
stat -c '%a' "$req" "$rep" "$(dirname "$req")" > "$LOG.modes"
case "$MODE" in
fail) exit 3 ;;
silent) sleep 5; exit 0 ;;
esac
exec 3>"$rep"
printf '{"v":1,"stage":"ready"}\n' >&3
IFS= read -r line < "$req"
printf '%s\n' "$line" > "$LOG.req"
case "$MODE" in
patchfailed) printf '{"v":1,"stage":"done","patchFailed":true,"error":"could not patch"}\n' >&3 ;;
hang) sleep 5 ;;
*) printf '{"v":1,"stage":"done","ok":true,"result":{"PID":5,"PersonaPatched":true}}\n' >&3 ;;
esac
`

type fakeRun struct {
	opts Options
	log  string
}

func newFakeRun(t *testing.T, mode string) fakeRun {
	t.Helper()
	dir := t.TempDir()
	runner := filepath.Join(dir, "umu-run")
	if err := os.WriteFile(runner, []byte(fakeUmu), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "log")
	t.Setenv("LOG", log)
	t.Setenv("MODE", mode)
	return fakeRun{
		log: log,
		opts: Options{
			Runner:     runner,
			Helper:     filepath.Join(dir, "bin", HelperName),
			Prefix:     filepath.Join(dir, "prefix"),
			ProtonPath: "/opt/GE-Proton",
			RunDir:     filepath.Join(dir, "run"),
			Launch: client.LaunchOptions{
				BinDir:      "/home/p/.local/share/AzothLauncher/client/Bin64",
				GatewayAddr: "play.example.test:6000",
				PingHost:    "play.example.test",
				Persona:     testTicket,
			},
			ReadyTimeout: 3 * time.Second,
			DoneTimeout:  time.Second,
		},
	}
}

func (f fakeRun) read(t *testing.T, ext string) string {
	t.Helper()
	b, err := os.ReadFile(f.log + ext)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (f fakeRun) pipesGone(t *testing.T) {
	t.Helper()
	left, _ := os.ReadDir(f.opts.RunDir)
	if len(left) != 0 {
		t.Fatalf("pipes left behind: %v", left)
	}
}

func TestLaunchOK(t *testing.T) {
	f := newFakeRun(t, "ok")
	rep, err := Launch(context.Background(), f.opts)
	if err != nil || !rep.OK || rep.Stage != patcher.StageDone || rep.Result == nil || rep.Result.PID != 5 {
		t.Fatalf("Launch = %+v %v", rep, err)
	}
	args, env := f.read(t, ".args"), f.read(t, ".env")
	if strings.Contains(args, testTicket) || strings.Contains(env, testTicket) {
		t.Fatal("the ticket must not be in the arguments or the environment")
	}
	if !strings.HasPrefix(args, f.opts.Helper+` -request Z:\`) || !strings.Contains(args, ` -reply Z:\`) {
		t.Fatalf("args = %q", args)
	}
	for _, want := range []string{"WINEPREFIX=" + f.opts.Prefix, "GAMEID=" + GameID, "PROTON_VERB=run", "PROTONPATH=/opt/GE-Proton"} {
		if !strings.Contains(env, want+"\n") {
			t.Fatalf("env misses %s", want)
		}
	}
	if modes := strings.Fields(f.read(t, ".modes")); len(modes) != 3 || modes[0] != "600" || modes[1] != "600" || modes[2] != "700" {
		t.Fatalf("pipe/dir modes = %v, want 600 600 700", modes)
	}
	req := f.read(t, ".req")
	if !strings.Contains(req, `"binDir":"Z:\\home\\p\\.local\\share\\AzothLauncher\\client\\Bin64"`) || !strings.Contains(req, testTicket) {
		t.Fatalf("request = %s", req)
	}
	f.pipesGone(t)
}

func TestLaunchWithoutProtonPath(t *testing.T) {
	f := newFakeRun(t, "ok")
	f.opts.ProtonPath = ""
	if _, err := Launch(context.Background(), f.opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.read(t, ".env"), "PROTONPATH=") {
		t.Fatal("without a setting umu picks UMU-Proton: no PROTONPATH")
	}
}

func TestLaunchPatchFailed(t *testing.T) {
	f := newFakeRun(t, "patchfailed")
	rep, err := Launch(context.Background(), f.opts)
	if err != nil || rep.OK || !rep.PatchFailed || rep.Error != "could not patch" {
		t.Fatalf("Launch = %+v %v", rep, err)
	}
	f.pipesGone(t)
}

func TestLaunchFailures(t *testing.T) {
	for mode, want := range map[string]string{
		"fail":   "failed before the patcher started",
		"silent": "did not start in time",
		"hang":   "did not finish in time",
	} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeRun(t, mode)
			_, err := Launch(context.Background(), f.opts)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want %q", err, want)
			}
			f.pipesGone(t)
		})
	}
}

func TestLaunchCancelled(t *testing.T) {
	f := newFakeRun(t, "silent")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	if _, err := Launch(ctx, f.opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestLaunchMissingRunner(t *testing.T) {
	f := newFakeRun(t, "ok")
	f.opts.Runner = filepath.Join(t.TempDir(), "no-umu-run")
	if _, err := Launch(context.Background(), f.opts); err == nil {
		t.Fatal("a missing umu-run must fail")
	}
	f.pipesGone(t)
}

func TestWinPath(t *testing.T) {
	for in, want := range map[string]string{
		"/home/p/.local/share/AzothLauncher/run/req-01": `Z:\home\p\.local\share\AzothLauncher\run\req-01`,
		"/mnt/My Games//New World/":                     `Z:\mnt\My Games\New World`,
	} {
		if got := WinPath(in); got != want {
			t.Errorf("WinPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestInstallHelper(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "launcher", HelperName)
	dst := filepath.Join(dir, "data", "bin", HelperName)
	exe := []byte("MZ fake helper")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, exe, 0o755); err != nil {
		t.Fatal(err)
	}
	want := sha(exe)

	if err := InstallHelper(src, dst, ""); !errors.Is(err, ErrNoHelperChecksum) {
		t.Fatalf("no checksum: %v", err)
	}
	if err := InstallHelper(src, dst, sha([]byte("other"))); !errors.Is(err, ErrHelperMismatch) {
		t.Fatalf("other checksum: %v", err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused helper must not be installed")
	}
	if err := InstallHelper(filepath.Join(dir, "nope.exe"), dst, want); !errors.Is(err, ErrHelperMissing) {
		t.Fatalf("missing: %v", err)
	}
	if err := InstallHelper(src, dst, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != string(exe) {
		t.Fatal("the helper was not copied")
	}

	// a changed copy in the data directory is replaced
	if err := os.WriteFile(dst, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := InstallHelper(src, dst, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != string(exe) {
		t.Fatal("a changed copy must be replaced")
	}
	if _, err := os.Stat(dst + ".tmp"); err == nil {
		t.Fatal("the .tmp file was left behind")
	}
}

func TestParsePingRange(t *testing.T) {
	for in, want := range map[string][2]int64{
		"0\t2147483647\n": {0, 2147483647},
		"1 0":             {1, 0},
		"100 100":         {100, 100},
	} {
		lo, hi, err := parsePingRange(in)
		if err != nil || lo != want[0] || hi != want[1] {
			t.Errorf("parsePingRange(%q) = %d %d %v", in, lo, hi, err)
		}
	}
	for _, bad := range []string{"", "1", "a b", "1 2 3"} {
		if _, _, err := parsePingRange(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestPingAllowedRuns(t *testing.T) {
	if _, err := os.Stat(pingRangePath); err != nil {
		t.Skip(err)
	}
	if _, err := PingAllowed(); err != nil {
		t.Fatal(err)
	}
}
