package options


import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"log"
	"github.com/google/go-cmp/cmp"
	"github.com/cyokozai/pvectl/app/cli"
)


func TestParseOptions(t *testing.T) {
	testCases := []struct {
		Input    []string
		Expected *Options
	}{
		{
			Input: []string{"-foo", "foo", "-bar", "bar"},
			Expected: &Options{
				Foo: "foo",
				Bar: "bar",
			},
		},
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

		opts, err := OptionParser(testCase.Input, &cli.InOut{
			StdIn:  strings.NewReader(""),
			StdOut: stdout,
			StdErr: stderr,
		})
		if err != nil {
			t.Fatalf("failed to parse options: %v", err)
			log.Println("Error parsing options:", err)
		}

		if !reflect.DeepEqual(opts, testCase.Expected) {
			t.Error(cmp.Diff(opts, testCase.Expected))
			log.Println("Parsed options do not match expected:", cmp.Diff(opts, testCase.Expected))
		}
	}
}