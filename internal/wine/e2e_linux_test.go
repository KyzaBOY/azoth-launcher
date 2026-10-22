package wine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/KyzaBOY/azoth-launcher/internal/client"
)

// gameExitWait is how long killGame waits for the killed processes to go.
const gameExitWait = 10 * time.Second

// TestEndToEnd runs the real helper through the real umu-run against the
// real game, when AZOTH_UMU_E2E=1 and AZOTH_CLIENT_BIN points at the Bin64
// folder of the supported build:
//
//	AZOTH_UMU_E2E=1 AZOTH_CLIENT_BIN=<game>/Bin64 [PROTONPATH=<proton>] \
//	  [AZOTH_E2E_PREFIX=<reused prefix>] go test ./internal/wine -run EndToEnd -v
//
// It builds azoth-patcher.exe, builds an isolated copy in the temp folder,
// launches with a test persona against a gateway where nothing listens and
// checks that every patch was written and read back. Then it kills the game.
func TestEndToEnd(t *testing.T) {
	bin := os.Getenv("AZOTH_CLIENT_BIN")
	if os.Getenv("AZOTH_UMU_E2E") != "1" || bin == "" {
		t.Skip("AZOTH_UMU_E2E=1 and AZOTH_CLIENT_BIN not set")
	}
	runner, err := exec.LookPath(Runner)
	if err != nil {
		t.Skip(err)
	}
	work := t.TempDir()

	helper := filepath.Join(work, "bin", HelperName)
	build := exec.Command("go", "build", "-o", helper, "github.com/KyzaBOY/azoth-launcher/cmd/azoth-patcher")
	build.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the helper: %v\n%s", err, out)
	}

	copyBin, err := client.Prepare(client.PrepareOptions{GameDir: filepath.Dir(bin), Dest: filepath.Join(work, "client")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(filepath.Join(work, "client", "assets")) }) // the link only, never the install's assets
	t.Cleanup(func() { killGame(t, copyBin) })

	prefix := os.Getenv("AZOTH_E2E_PREFIX")
	if prefix == "" {
		prefix = filepath.Join(work, "prefix")
	}
	const persona = "azoth-e2e-persona"
	rep, err := Launch(context.Background(), Options{
		Runner:     runner,
		Helper:     helper,
		Prefix:     prefix,
		ProtonPath: os.Getenv("PROTONPATH"),
		RunDir:     filepath.Join(work, "run"),
		Launch: client.LaunchOptions{
			BinDir:      copyBin,
			GatewayAddr: "127.0.0.1:9",
			PingHost:    "127.0.0.1",
			Persona:     persona,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK || rep.Result == nil {
		t.Fatalf("launch failed: %s (patchFailed=%v)", rep.Error, rep.PatchFailed)
	}
	r := rep.Result
	t.Logf("result: %+v", *r)
	if !r.PatchedInMem || !r.PingPatchedInMem || r.PingHost != "127.0.0.1" || !r.PersonaPatched {
		t.Fatalf("not every patch was confirmed: %+v", r)
	}
	if r.BytesAfter != client.Hex(client.PatchedBytes[:]) {
		t.Fatalf("dtls bytes after = %s", r.BytesAfter)
	}
}

// killGame kills every process that runs from the copy's Bin64 (the game and
// its crash uploader; Wine shows their paths as Z:\...) and waits until they
// are gone, so the temp folder can be removed.
func killGame(t *testing.T, copyBin string) {
	want := strings.ToLower(WinPath(copyBin) + `\`)
	procs, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	var killed []string
	for _, p := range procs {
		cmd, err := os.ReadFile(p)
		if err != nil || !strings.Contains(strings.ToLower(string(cmd)), want) {
			continue
		}
		pid, err := strconv.Atoi(filepath.Base(filepath.Dir(p)))
		if err == nil && syscall.Kill(pid, syscall.SIGKILL) == nil {
			t.Logf("killed pid %d", pid)
			killed = append(killed, filepath.Dir(p))
		}
	}
	deadline := time.Now().Add(gameExitWait)
	for _, dir := range killed {
		for time.Now().Before(deadline) {
			if _, err := os.Stat(dir); err != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}
