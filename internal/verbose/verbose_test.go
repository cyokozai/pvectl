package verbose

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestNewDisabled(t *testing.T) {
	t.Run("level 0 yields a nil logger", func(t *testing.T) {
		if l := New(0, &bytes.Buffer{}); l != nil {
			t.Fatalf("New(0) = %v, want nil", l)
		}
	})
	t.Run("negative level yields a nil logger", func(t *testing.T) {
		if l := New(-3, &bytes.Buffer{}); l != nil {
			t.Fatalf("New(-3) = %v, want nil", l)
		}
	})
	t.Run("nil writer yields a nil logger", func(t *testing.T) {
		if l := New(9, nil); l != nil {
			t.Fatalf("New(9, nil) = %v, want nil", l)
		}
	})
	t.Run("every method tolerates a nil logger", func(t *testing.T) {
		var l *Logger
		l.Secret("a-secret-value")
		l.Logf(1, "nothing happens")
		if l.Enabled(1) {
			t.Error("nil logger reports level 1 enabled")
		}
		if l.Level() != 0 {
			t.Errorf("nil logger Level() = %d, want 0", l.Level())
		}
		if got := l.Scrub("a-secret-value"); got != "a-secret-value" {
			t.Errorf("nil logger Scrub() = %q, want the input unchanged", got)
		}
	})
}

func TestLogfLevelGate(t *testing.T) {
	var buf bytes.Buffer
	l := New(6, &buf)

	l.Logf(LevelRequest, "at the level")
	l.Logf(LevelHeaders, "above the level")
	l.Logf(LevelContext, "below the level")

	got := buf.String()
	if !strings.Contains(got, "[v6] at the level") {
		t.Errorf("missing the at-level message:\n%s", got)
	}
	if !strings.Contains(got, "[v1] below the level") {
		t.Errorf("missing the below-level message:\n%s", got)
	}
	if strings.Contains(got, "above the level") {
		t.Errorf("wrote a message above the configured level:\n%s", got)
	}
}

func TestLogfPrefixesEveryLine(t *testing.T) {
	var buf bytes.Buffer
	New(9, &buf).Logf(8, "first\nsecond\n")

	want := "[v8] first\n[v8] second\n"
	if got := buf.String(); got != want {
		t.Errorf("Logf() = %q, want %q", got, want)
	}
}

func TestScrub(t *testing.T) {
	t.Run("registered secrets are replaced everywhere", func(t *testing.T) {
		var buf bytes.Buffer
		l := New(9, &buf)
		l.Secret("00000000-0000-0000-0000-000000000000")
		l.Logf(1, "Authorization: PVEAPIToken=ci@pam!id=%s", "00000000-0000-0000-0000-000000000000")

		got := buf.String()
		if strings.Contains(got, "00000000-0000-0000-0000-000000000000") {
			t.Errorf("secret survived Logf:\n%s", got)
		}
		if !strings.Contains(got, Redacted) {
			t.Errorf("no %s in output:\n%s", Redacted, got)
		}
	})

	t.Run("short values are not swept", func(t *testing.T) {
		l := New(9, &bytes.Buffer{})
		l.Secret("ab")
		if got := l.Scrub("a table with ab in it"); got != "a table with ab in it" {
			t.Errorf("Scrub() = %q, want the input unchanged", got)
		}
	})

	t.Run("a longer secret containing a shorter one is replaced whole", func(t *testing.T) {
		l := New(9, &bytes.Buffer{})
		l.Secret("secret-value")
		l.Secret("secret-value-and-more")
		if got := l.Scrub("x secret-value-and-more x"); got != "x "+Redacted+" x" {
			t.Errorf("Scrub() = %q, want the whole longer secret replaced", got)
		}
	})

	t.Run("an empty value is ignored", func(t *testing.T) {
		l := New(9, &bytes.Buffer{})
		l.Secret("")
		if got := l.Scrub("unchanged"); got != "unchanged" {
			t.Errorf("Scrub() = %q", got)
		}
	})
}

// TestConcurrentUse exercises the paths -race cares about: the
// transport registers ticket secrets from a response while other
// goroutines are writing lines.
func TestConcurrentUse(t *testing.T) {
	var buf bytes.Buffer
	l := New(9, &buf)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l.Secret("ticket-value-that-is-long-enough")
			l.Logf(6, "line %d carrying ticket-value-that-is-long-enough", i)
		}(i)
	}
	wg.Wait()

	if strings.Contains(buf.String(), "ticket-value-that-is-long-enough") {
		t.Error("a secret registered concurrently survived into the output")
	}
}
