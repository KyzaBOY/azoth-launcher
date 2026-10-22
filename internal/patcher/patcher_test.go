package patcher

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/KyzaBOY/azoth-launcher/internal/client"
)

func reader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }

func TestRequestRoundTrip(t *testing.T) {
	want := Request{V: Version, BinDir: `Z:\home\p\.local\share\AzothLauncher\client\Bin64`, GatewayAddr: "play.example.test:6000", PingHost: "play.example.test", Persona: "ticket-abc", RegionName: "NA"}
	var buf bytes.Buffer
	if err := WriteLine(&buf, want); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) || bytes.Count(buf.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("not one line: %q", buf.String())
	}
	got, err := ReadRequest(bufio.NewReader(&buf))
	if err != nil || got != want {
		t.Fatalf("round trip = %+v %v", got, err)
	}
}

func TestReplyRoundTripHasNoTicket(t *testing.T) {
	want := Reply{V: Version, Stage: StageDone, OK: true, Result: &client.LaunchResult{PID: 32, PersonaPatched: true, PingHost: "play.example.test"}}
	var buf bytes.Buffer
	if err := WriteLine(&buf, want); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(buf.String()), "persona\"") {
		t.Fatalf("a reply has no persona field: %s", buf.String())
	}
	got, err := ReadReply(bufio.NewReader(&buf))
	if err != nil || got.Stage != StageDone || !got.OK || got.Result == nil || *got.Result != *want.Result {
		t.Fatalf("round trip = %+v %v", got, err)
	}
}

func TestReadRequestRefusals(t *testing.T) {
	long := `{"v":1,"binDir":"` + strings.Repeat("x", MaxRequestLine) + `","gatewayAddr":"h:1"}` + "\n"
	for name, c := range map[string]struct {
		in   string
		want error
	}{
		"wrong version": {`{"v":2,"binDir":"Z:\\b","gatewayAddr":"h:1"}` + "\n", ErrVersion},
		"no version":    {`{"binDir":"Z:\\b","gatewayAddr":"h:1"}` + "\n", ErrVersion},
		"too long":      {long, ErrLineTooLong},
		"empty input":   {"", io.EOF},
		"no newline":    {`{"v":1,"binDir":"Z:\\b","gatewayAddr":"h:1"}`, io.EOF},
	} {
		if _, err := ReadRequest(reader(c.in)); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	for name, in := range map[string]string{
		"bad json":   "{not json\n",
		"no bin dir": `{"v":1,"gatewayAddr":"h:1"}` + "\n",
		"no gateway": `{"v":1,"binDir":"Z:\\b"}` + "\n",
		"json array": "[1,2]\n",
		"blank line": "\n",
	} {
		if _, err := ReadRequest(reader(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestReadReplyRefusals(t *testing.T) {
	for name, in := range map[string]string{
		"wrong version": `{"v":3,"stage":"ready"}` + "\n",
		"no stage":      `{"v":1}` + "\n",
		"odd stage":     `{"v":1,"stage":"launched"}` + "\n",
		"too long":      `{"v":1,"stage":"done","error":"` + strings.Repeat("e", MaxReplyLine) + `"}` + "\n",
	} {
		if _, err := ReadReply(reader(in)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Two replies in a row are read one at a time.
func TestReadReplySequence(t *testing.T) {
	r := reader(`{"v":1,"stage":"ready"}` + "\n" + `{"v":1,"stage":"done","patchFailed":true,"error":"x"}` + "\n")
	first, err := ReadReply(r)
	if err != nil || first.Stage != StageReady {
		t.Fatalf("first = %+v %v", first, err)
	}
	second, err := ReadReply(r)
	if err != nil || second.Stage != StageDone || !second.PatchFailed || second.Error != "x" {
		t.Fatalf("second = %+v %v", second, err)
	}
}
