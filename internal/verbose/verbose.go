// Package verbose implements pvectl's leveled debug output: the level
// constants callers log against, the writer (always stderr, never
// stdout), and the redaction that keeps credentials out of what users
// paste into bug reports.
//
// The level scale follows kubectl's -v: low levels explain what the
// tool decided, level 6 and up expose the HTTP conversation in
// increasing detail. A Logger is nil when -v is 0, and every method is
// nil-safe, so callers never guard their log calls.
package verbose

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
)

// Redacted is what a masked value is replaced with. It is the same
// literal "pvectl config view" prints, so there is one string to
// recognise across all of pvectl's output.
const Redacted = "REDACTED"

// Verbosity levels. 1-5 report pvectl's own decisions, 6-9 the HTTP
// traffic, mirroring kubectl's split at 6.
const (
	// LevelContext reports which config file, context, node, and user
	// the invocation resolved to.
	LevelContext = 1
	// LevelResolve reports how names became vmids and nodes.
	LevelResolve = 2
	// LevelPlan reports what apply/diff decided: the action taken and
	// the managed keys judged to have changed.
	LevelPlan = 3
	// LevelTask reports the start and completion of Proxmox tasks.
	LevelTask = 4
	// LevelParams reports the flat Proxmox parameter maps pvectl built,
	// before they become an HTTP body.
	LevelParams = 5
	// LevelRequest reports one line per HTTP request: method, URL,
	// status, elapsed time.
	LevelRequest = 6
	// LevelHeaders adds request and response headers.
	LevelHeaders = 7
	// LevelBody adds request and response bodies, truncated.
	LevelBody = 8
	// LevelFullBody adds bodies without truncation.
	LevelFullBody = 9
)

// minSecretLen bounds the literal sweep: shorter values are too likely
// to occur as ordinary substrings (a two-character password would turn
// unrelated output into confetti). Real Proxmox credentials — API token
// secrets, tickets, CSRF tokens — are far longer than this.
const minSecretLen = 8

// Logger writes leveled messages to a single writer, scrubbing known
// secret literals from every line on the way out.
//
// The zero value is not usable; use New. A nil *Logger is usable and
// discards everything, which is how -v=0 is represented.
type Logger struct {
	level int

	mu      sync.Mutex
	out     io.Writer
	secrets []string
}

// New returns a Logger writing to out, or nil when nothing should be
// logged. out must be stderr (or a test buffer standing in for it):
// debug output on stdout would corrupt -o yaml / -o json.
func New(level int, out io.Writer) *Logger {
	if level < LevelContext || out == nil {
		return nil
	}
	return &Logger{level: level, out: out}
}

// Level returns the configured verbosity, 0 for a disabled logger.
func (l *Logger) Level() int {
	if l == nil {
		return 0
	}
	return l.level
}

// Enabled reports whether messages at the given level are written.
// Callers use it to skip building expensive messages.
func (l *Logger) Enabled(level int) bool {
	return l != nil && level <= l.level
}

// Secret registers literals to replace with Redacted in everything this
// logger writes. It is the backstop behind the per-key and per-header
// masking: a credential that reaches the logger by an unforeseen route
// is still removed, because the logger knows the credential itself.
func (l *Logger) Secret(values ...string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, v := range values {
		if len(v) < minSecretLen {
			continue
		}
		if !slices.Contains(l.secrets, v) {
			l.secrets = append(l.secrets, v)
		}
	}
	// Longest first: a credential that contains a shorter registered
	// literal must be replaced whole, not left as a half-masked string.
	slices.SortStableFunc(l.secrets, func(a, b string) int { return len(b) - len(a) })
}

// Scrub replaces every registered secret in s with Redacted. Logf calls
// it already; callers only need it when they transform a string (e.g.
// truncate it) after the secrets would otherwise have been caught.
func (l *Logger) Scrub(s string) string {
	if l == nil {
		return s
	}
	l.mu.Lock()
	secrets := append([]string(nil), l.secrets...)
	l.mu.Unlock()
	for _, secret := range secrets {
		s = strings.ReplaceAll(s, secret, Redacted)
	}
	return s
}

// contextKey is the private key the logger is stored under.
type contextKey struct{}

// NewContext returns a context carrying the logger. Resource handlers
// receive only a context and an api.Client (that is the extension point
// ADR-006 fixed), so this is how they reach the logger without every
// handler signature growing a parameter.
func NewContext(ctx context.Context, log *Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, log)
}

// FromContext returns the logger the context carries, or nil — which is
// a working, silent logger.
func FromContext(ctx context.Context) *Logger {
	log, _ := ctx.Value(contextKey{}).(*Logger)
	return log
}

// Logf writes one message at the given level. A message spanning
// several lines is written as several prefixed lines, so no line of
// debug output is ever mistaken for program output.
func (l *Logger) Logf(level int, format string, args ...any) {
	if !l.Enabled(level) {
		return
	}
	msg := l.Scrub(fmt.Sprintf(format, args...))
	prefix := fmt.Sprintf("[v%d] ", level)

	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range strings.Split(strings.TrimRight(msg, "\n"), "\n") {
		fmt.Fprint(l.out, prefix+line+"\n")
	}
}
