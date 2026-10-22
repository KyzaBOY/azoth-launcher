// Package patcher is the protocol between the Linux launcher and
// azoth-patcher.exe, the small Windows helper that runs inside Wine/Proton and
// does the one thing Linux cannot: start NewWorld.exe suspended, patch it in
// memory and resume it (client.Launch).
//
// The two talk over two FIFOs (named pipes) made by the launcher with mode 0600,
// one JSON object per line:
//
//  1. the helper writes Reply{Stage: StageReady} on the reply pipe;
//  2. the helper reads one Request from the request pipe;
//  3. the helper launches and writes Reply{Stage: StageDone} with the result.
//
// The ticket (Request.Persona) only ever travels through the request pipe:
// never on a command line, in a file or in a log. Stdin is not used because
// Proton gives it to its own setup steps before the helper starts.
package patcher

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/KyzaBOY/azoth-launcher/internal/client"
)

const (
	// Version is the protocol version. Both sides refuse any other.
	Version = 1

	// MaxRequestLine caps a request line: a path, a host, a ticket and the region name.
	MaxRequestLine = 4 << 10
	// MaxReplyLine caps a reply line (a LaunchResult is far smaller).
	MaxReplyLine = 16 << 10

	// StageReady: the helper runs and waits for the request.
	StageReady = "ready"
	// StageDone: the launch is over; OK, PatchFailed, Error and Result tell how.
	StageDone = "done"
)

// Request is what the launcher asks the helper to launch.
type Request struct {
	V           int    `json:"v"`
	BinDir      string `json:"binDir"`      // Windows path of the copy's Bin64 (Z:\...)
	GatewayAddr string `json:"gatewayAddr"` // host:port
	PingHost    string `json:"pingHost"`    // patch #3
	Persona     string `json:"persona"`     // patch #4: the Discord ticket
	RegionName  string `json:"regionName"`  // patch #5
}

// Reply is a message from the helper. It never carries the ticket.
type Reply struct {
	V           int                  `json:"v"`
	Stage       string               `json:"stage"`
	OK          bool                 `json:"ok,omitempty"`
	PatchFailed bool                 `json:"patchFailed,omitempty"` // client.ErrPatchFailed: the game was killed before it ran
	Error       string               `json:"error,omitempty"`
	Result      *client.LaunchResult `json:"result,omitempty"`
}

var (
	// ErrLineTooLong: a line went past its cap.
	ErrLineTooLong = errors.New("protocol line too long")
	// ErrVersion: the other side speaks another protocol version.
	ErrVersion = errors.New("unsupported protocol version")
)

// WriteLine writes v as one JSON line.
func WriteLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// ReadRequest reads and checks one request line.
func ReadRequest(r *bufio.Reader) (Request, error) {
	var req Request
	if err := readLine(r, MaxRequestLine, &req); err != nil {
		return Request{}, err
	}
	if req.V != Version {
		return Request{}, fmt.Errorf("%w: %d", ErrVersion, req.V)
	}
	if req.BinDir == "" || req.GatewayAddr == "" {
		return Request{}, errors.New("incomplete request")
	}
	return req, nil
}

// ReadReply reads and checks one reply line.
func ReadReply(r *bufio.Reader) (Reply, error) {
	var rep Reply
	if err := readLine(r, MaxReplyLine, &rep); err != nil {
		return Reply{}, err
	}
	if rep.V != Version {
		return Reply{}, fmt.Errorf("%w: %d", ErrVersion, rep.V)
	}
	if rep.Stage != StageReady && rep.Stage != StageDone {
		return Reply{}, fmt.Errorf("unknown stage %q", rep.Stage)
	}
	return rep, nil
}

// readLine reads up to and including '\n' (at most max bytes) and decodes it into v.
func readLine(r *bufio.Reader, max int, v any) error {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > max {
			return ErrLineTooLong
		}
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return err
		}
	}
	if err := json.Unmarshal(line, v); err != nil {
		return fmt.Errorf("invalid protocol line: %w", err)
	}
	return nil
}
