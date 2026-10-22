//go:build windows

// azoth-patcher is the Windows helper of the Linux launcher. It runs inside
// Wine/Proton (started by umu-run) and only calls client.Launch: start
// NewWorld.exe suspended, apply and confirm the in-memory patches, resume it
// (or kill it). It takes its request from the launcher over a FIFO and does
// nothing without it; see internal/patcher for the protocol.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/KyzaBOY/azoth-launcher/internal/client"
	"github.com/KyzaBOY/azoth-launcher/internal/patcher"
)

const (
	exitOK       = 0
	exitFailed   = 1 // the launch failed; the reply says why
	exitRefused  = 2 // no handshake: nothing was started
	requestWait  = 30 * time.Second
	usageRefusal = "azoth-patcher is started by the Azoth launcher and does nothing on its own."
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr, client.Launch))
}

// run is the whole helper; launch is client.Launch (tests replace it).
func run(args []string, stderr io.Writer, launch func(client.LaunchOptions) (*client.LaunchResult, error)) int {
	fs := flag.NewFlagSet("azoth-patcher", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	requestPath := fs.String("request", "", "request pipe (Windows path)")
	replyPath := fs.String("reply", "", "reply pipe (Windows path)")
	if err := fs.Parse(args); err != nil || *requestPath == "" || *replyPath == "" || fs.NArg() != 0 {
		fmt.Fprintln(stderr, usageRefusal)
		return exitRefused
	}

	reply, err := os.OpenFile(*replyPath, os.O_WRONLY, 0)
	if err != nil {
		fmt.Fprintln(stderr, usageRefusal)
		return exitRefused
	}
	defer reply.Close()
	if err := patcher.WriteLine(reply, patcher.Reply{V: patcher.Version, Stage: patcher.StageReady}); err != nil {
		return exitRefused
	}

	req, err := readRequest(*requestPath, requestWait)
	if err != nil {
		fmt.Fprintln(stderr, usageRefusal)
		return exitRefused
	}

	// No Log: Proton keeps the helper's output in its log, and the steps
	// mention the gateway. The ticket is never part of any step anyway.
	res, err := launch(client.LaunchOptions{
		BinDir:      req.BinDir,
		GatewayAddr: req.GatewayAddr,
		PingHost:    req.PingHost,
		Persona:     req.Persona,
	})
	done := patcher.Reply{V: patcher.Version, Stage: patcher.StageDone, OK: err == nil, Result: res}
	if err != nil {
		done.Error = err.Error()
		done.PatchFailed = errors.Is(err, client.ErrPatchFailed)
	}
	if werr := patcher.WriteLine(reply, done); werr != nil || err != nil {
		return exitFailed
	}
	return exitOK
}

// readRequest reads the one request line, giving up after wait (the open of a
// pipe without a writer, or a writer that never sends, would block forever).
func readRequest(path string, wait time.Duration) (patcher.Request, error) {
	type result struct {
		req patcher.Request
		err error
	}
	got := make(chan result, 1)
	go func() {
		f, err := os.Open(path)
		if err != nil {
			got <- result{err: err}
			return
		}
		defer f.Close()
		req, err := patcher.ReadRequest(bufio.NewReader(f))
		got <- result{req, err}
	}()
	select {
	case r := <-got:
		return r.req, r.err
	case <-time.After(wait):
		return patcher.Request{}, errors.New("no request")
	}
}
