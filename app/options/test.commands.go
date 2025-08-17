package options


import (
	"bytes"
	"strings"
	"testing"
	"log"
	"github.com/cyokozai/pvectl/app/cli"
)


func TestMainCommand(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	err := MainCommandByOptions(&Options{
		Foo: "foo",
		Bar: "bar",
	}, &cli.InOut{
		StdIn:  strings.NewReader(""),
		StdOut: stdout,
		StdErr: stderr,
	})
	if err != nil {
		t.Fatalf("failed to run task: %v", err)
		log.Println("Error running task:", err)
	}

	expected := "foo: foo\nbar: bar\n"
	if stdout.String() != expected {
		t.Errorf("want %q, got %q", expected, stdout.String())
		log.Println("Unexpected output:", stdout.String())
	}
}