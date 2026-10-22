// Package wine is the Linux side of the launch: it runs azoth-patcher.exe
// inside Wine/Proton through umu-run and talks to it over two FIFOs (see
// internal/patcher). Everything else of the launcher runs natively.
package wine

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/KyzaBOY/azoth-launcher/internal/client"
	"github.com/KyzaBOY/azoth-launcher/internal/patcher"
)

const (
	// Runner is umu-launcher's command.
	Runner = "umu-run"
	// GameID is New World's Steam id in umu's form, so umu-protonfixes can
	// apply the fixes it has for the game.
	GameID = "umu-1063730"

	// DefaultReadyTimeout covers the first run: umu may download UMU-Proton
	// and the Steam runtime, and Proton creates the prefix.
	DefaultReadyTimeout = 10 * time.Minute
	// DefaultDoneTimeout covers the launch itself (a few seconds).
	DefaultDoneTimeout = 2 * time.Minute

	pipeNameBytes = 8 // random part of a pipe name
)

// Options describes one run of the helper.
type Options struct {
	Runner     string // path of umu-run
	Helper     string // path of azoth-patcher.exe
	Prefix     string // WINEPREFIX (created by umu on the first run)
	ProtonPath string // PROTONPATH; "" = umu's UMU-Proton
	RunDir     string // where the pipes are made (mode 0700)
	Launch     client.LaunchOptions

	ReadyTimeout time.Duration // 0 = DefaultReadyTimeout
	DoneTimeout  time.Duration // 0 = DefaultDoneTimeout
}

// WinPath is the Windows path Wine gives a Linux path (drive Z: is /).
func WinPath(unix string) string {
	return "Z:" + strings.ReplaceAll(filepath.Clean(unix), "/", `\`)
}

// Launch runs the helper and returns its final reply. The ticket
// (o.Launch.Persona) goes only into the request pipe: it is not in the
// helper's arguments, in its environment or in any file.
func Launch(ctx context.Context, o Options) (patcher.Reply, error) {
	if o.ReadyTimeout <= 0 {
		o.ReadyTimeout = DefaultReadyTimeout
	}
	if o.DoneTimeout <= 0 {
		o.DoneTimeout = DefaultDoneTimeout
	}
	reqPath, repPath, err := makePipes(o.RunDir)
	if err != nil {
		return patcher.Reply{}, err
	}
	defer os.Remove(reqPath)
	defer os.Remove(repPath)

	// Both ends are opened read-write: such an open of a FIFO never blocks,
	// and the request waits in the pipe until the helper reads it.
	req, err := os.OpenFile(reqPath, os.O_RDWR, 0)
	if err != nil {
		return patcher.Reply{}, err
	}
	defer req.Close()
	rep, err := os.OpenFile(repPath, os.O_RDWR, 0)
	if err != nil {
		return patcher.Reply{}, err
	}
	defer rep.Close() // also ends readReplies
	if err := patcher.WriteLine(req, patcher.Request{
		V:           patcher.Version,
		BinDir:      WinPath(o.Launch.BinDir),
		GatewayAddr: o.Launch.GatewayAddr,
		PingHost:    o.Launch.PingHost,
		Persona:     o.Launch.Persona,
		RegionName:  o.Launch.RegionName,
	}); err != nil {
		return patcher.Reply{}, err
	}

	cmd := exec.Command(o.Runner, o.Helper, "-request", WinPath(reqPath), "-reply", WinPath(repPath))
	cmd.Env = append(os.Environ(), env(o)...)
	if err := cmd.Start(); err != nil {
		return patcher.Reply{}, fmt.Errorf("start %s: %w", Runner, err)
	}
	// umu-run lives as long as the game: it is waited for in the background.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	replies := make(chan replyOrError, 2)
	go readReplies(rep, replies)

	ready := false
	timer := time.NewTimer(o.ReadyTimeout)
	defer timer.Stop()
	for {
		select {
		case r := <-replies:
			if r.err != nil {
				return patcher.Reply{}, fmt.Errorf("the patcher stopped answering: %w", r.err)
			}
			if r.reply.Stage == patcher.StageDone {
				return r.reply, nil
			}
			ready = true
			timer.Reset(o.DoneTimeout)
		case err := <-exited:
			exited = nil // umu-run may return before the helper is done (Proton's start.exe)
			if err != nil && !ready {
				return patcher.Reply{}, fmt.Errorf("%s failed before the patcher started: %w", Runner, err)
			}
		case <-timer.C:
			if !ready {
				return patcher.Reply{}, errors.New("the patcher did not start in time")
			}
			return patcher.Reply{}, errors.New("the patcher did not finish in time")
		case <-ctx.Done():
			return patcher.Reply{}, ctx.Err()
		}
	}
}

// env is what umu-run needs on top of the launcher's environment.
func env(o Options) []string {
	e := []string{"WINEPREFIX=" + o.Prefix, "GAMEID=" + GameID, "PROTON_VERB=run"}
	if o.ProtonPath != "" {
		e = append(e, "PROTONPATH="+o.ProtonPath)
	}
	return e
}

// makePipes creates the request and reply FIFOs, readable by this user only,
// with random names in dir (made with mode 0700).
func makePipes(dir string) (req, rep string, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", "", err
	}
	var raw [pipeNameBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", err
	}
	id := hex.EncodeToString(raw[:])
	req, rep = filepath.Join(dir, "req-"+id), filepath.Join(dir, "rep-"+id)
	if err := syscall.Mkfifo(req, 0o600); err != nil {
		return "", "", fmt.Errorf("make pipe: %w", err)
	}
	if err := syscall.Mkfifo(rep, 0o600); err != nil {
		_ = os.Remove(req)
		return "", "", fmt.Errorf("make pipe: %w", err)
	}
	return req, rep, nil
}

type replyOrError struct {
	reply patcher.Reply
	err   error
}

// readReplies reads the ready and the done reply. It ends with the first
// error, which closing f causes too.
func readReplies(f *os.File, out chan<- replyOrError) {
	r := bufio.NewReader(f)
	for {
		rep, err := patcher.ReadReply(r)
		out <- replyOrError{rep, err}
		if err != nil || rep.Stage == patcher.StageDone {
			return
		}
	}
}
