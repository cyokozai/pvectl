package options


import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	// "log"
	"github.com/google/go-cmp/cmp"
	"github.com/cyokozai/pvectl/app/cli"
)


func TestOptionParser_Success(t *testing.T) {
	testCases := []struct {
		Input    []string
		Expected *Options
	}{
		{
			Input: []string{"-h"},
			Expected: &Options{
				Help: true,
			},
		},
	}

	for _, testCase := range testCases {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		options, err := OptionParser(testCase.Input, &cli.InOut{
			StdIn:  strings.NewReader(""),
			StdOut: stdout,
			StdErr: stderr,
		})
		if err != nil {
			t.Fatalf("failed to parse options: %v", err)
		}

		if !reflect.DeepEqual(options, testCase.Expected) {
			t.Error(cmp.Diff(options, testCase.Expected))
		}
	}
}

func TestOptionParser_Error(t *testing.T) {
	testCases := []struct {
		Input []string
	}{
		{
			Input: []string{},
		},
	}

	for _, testCase := range testCases {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}

		_, err := OptionParser(testCase.Input, &cli.InOut{
			StdIn:  strings.NewReader(""),
			StdOut: stdout,
			StdErr: stderr,
		})
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	}
}

func testFlagUsage(t *testing.T) {
	stdin := strings.NewReader("")
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	_, err := OptionParser([]string{"-h"}, &cli.InOut{
		StdIn:  stdin,
		StdOut: &stdout,
		StdErr: &stderr,
	})
	if err != nil {
		t.Fatalf("failed to parse options: %v", err)
	}

	expected := `Usage: pvectl [options]
OPTIONS
  -bar string
    	bar
  -foo string
    	foo
`

	if stderr.String() != expected {
		t.Errorf("want %q, got %q", expected, stderr.String())
	}
}
