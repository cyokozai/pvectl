package options


import (
	"bytes"
	"strings"
	"testing"
	"github.com/cyokozai/pvectl/app/cli"
)

func TestMainCommandByOptions(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := MainCommandByOptions(&Options{}, &cli.InOut{
		StdIn:  strings.NewReader(""),
		StdOut: stdout,
		StdErr: stderr,
	})
	if err != nil {
		t.Fatalf("failed to run task: %v", err)
	}

	expected := "something-required: something\n"
	if stdout.String() != expected {
		t.Errorf("want %q, got %q", expected, stdout.String())
	}
}