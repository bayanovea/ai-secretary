// Command secretary keeps a Markdown artifact of duties and tells you which of
// them need attention today.
//
// The binary is deliberately thin: every decision it makes lives in one of the
// internal packages, and everything fuzzy — turning «hand the papers to the
// notary by Friday» into flags — happens before it, in the Claude Code skill. What is
// left here is argument parsing, one load/mutate/save cycle per subcommand, and
// the exit code.
//
// The clock is an argument. `--today` overrides it, which is what lets the
// integration tests assert on a briefing without waiting for a deadline to
// pass; without the flag the real date is used, truncated to a calendar day so
// that a command run at midnight behaves like one run at noon.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bayanovea/secretary/internal/config"
	"github.com/bayanovea/secretary/internal/mdstore"
	"github.com/bayanovea/secretary/internal/voice"
)

// Exit codes. Usage failures are told apart from runtime ones so that a script
// can distinguish "I called it wrong" from "it refused to do the thing".
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// defaultConfigPath is where the configuration is looked for when `--config` is
// not given. A missing file is not an error — config.Load falls back to the
// defaults — so the binary works in a fresh directory.
const defaultConfigPath = "secretary.yml"

// homeEnv names the directory a private installation keeps its configuration
// and artifact in, so the binary can run from any directory without flags.
// Unset, the current directory is the home — which is what makes a checkout
// of this repository a working example.
const homeEnv = "SECRETARY_HOME"

// errUsage marks a failure that is about how the command was called rather than
// about the duties themselves.
var errUsage = errors.New("invalid usage")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with its I/O and its exit code made testable.
func run(args []string, stdout, stderr io.Writer) int {
	g := globals{Config: defaultConfigPath}
	if home := strings.TrimSpace(os.Getenv(homeEnv)); home != "" {
		g.Home = home
		g.Config = filepath.Join(home, defaultConfigPath)
	}

	root := newFlagSet("secretary", stderr)
	g.register(root)
	root.Usage = func() { usage(stderr) }
	if err := root.Parse(args); err != nil {
		return exitUsage
	}

	rest := root.Args()
	if len(rest) == 0 {
		usage(stderr)
		return exitUsage
	}

	cmd := lookup(rest[0])
	if cmd == nil {
		fmt.Fprintf(stderr, "secretary: unknown command %q\n\n", rest[0])
		usage(stderr)
		return exitUsage
	}

	fs := newFlagSet("secretary "+cmd.name(), stderr)
	cmd.register(fs)
	// Registering the global flags a second time keeps `secretary add --today=…`
	// working alongside `secretary --today=… add`. Each flag's default is the
	// value the root parse produced, so the second registration cannot undo the
	// first.
	g.register(fs)
	fs.Usage = func() { commandUsage(stderr, cmd, fs) }
	positional, err := parseInterspersed(fs, rest[1:])
	if err != nil {
		return exitUsage
	}

	a, err := g.app(stdout)
	if err != nil {
		return fail(stderr, err)
	}
	if err := cmd.run(a, positional); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

// parseInterspersed parses flags that appear after a positional argument, which
// the standard flag package stops at.
//
// It matters because `secretary defer T-002 --due=2026-09-10` is how a person
// writes it — the duty first, then what to do with it — and refusing that
// ordering would be an implementation detail leaking into the interface.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// fail reports err and picks the exit code that matches it.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "secretary: %v\n", err)
	if errors.Is(err, errUsage) {
		return exitUsage
	}
	return exitError
}

// newFlagSet returns a flag set that reports its own errors and leaves the exit
// code to the caller.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// globals are the flags every subcommand accepts.
type globals struct {
	Config   string
	Artifact string
	// Home is the directory relative paths in the configuration resolve
	// against. Empty means the current directory, as a config file read by
	// hand would expect.
	Home string
	// Today overrides the clock. It is deliberately absent from the usage text:
	// it exists so that tests — and a future cron job replaying a day — are
	// deterministic, not as something to reach for by hand.
	Today string
}

// register adds the global flags to fs, taking the current values as defaults
// so that a second registration preserves what the first one parsed.
func (g *globals) register(fs *flag.FlagSet) {
	fs.StringVar(&g.Config, "config", g.Config, "path to secretary.yml")
	fs.StringVar(&g.Artifact, "artifact", g.Artifact, "path to the artifact (default from the configuration)")
	fs.StringVar(&g.Today, "today", g.Today, "")
}

// app resolves the globals into everything a subcommand needs: the
// configuration, the store over the artifact, and today's date.
func (g globals) app(stdout io.Writer) (*app, error) {
	cfg, err := config.Load(g.Config)
	if err != nil {
		return nil, err
	}

	now, err := resolveToday(g.Today)
	if err != nil {
		return nil, err
	}

	path := g.Artifact
	if path == "" {
		path = g.resolve(cfg.Artifact)
	}
	cfg.Archive = g.resolve(cfg.Archive)
	return &app{cfg: cfg, store: mdstore.NewStore(path, cfg), now: now, out: stdout}, nil
}

// resolve anchors a relative path from the configuration at the home directory.
// An explicit `--artifact` never passes through here: a path typed on the
// command line is relative to where it was typed.
func (g globals) resolve(path string) string {
	if g.Home == "" || path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(g.Home, path)
}

// resolveToday reads the `--today` flag, falling back to the real clock. Either
// way the result is a calendar day in UTC, so that a date written by the CLI
// compares exactly with one parsed out of the artifact.
func resolveToday(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return day(time.Now()), nil
	}
	t, err := time.Parse(mdstore.DateLayout, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: --today %q: expected YYYY-MM-DD", errUsage, value)
	}
	return day(t), nil
}

func day(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// app is one command's view of the world.
type app struct {
	cfg   *config.Config
	store *mdstore.Store
	now   time.Time
	out   io.Writer
}

// load parses the artifact.
func (a *app) load() (*mdstore.Document, error) {
	return a.store.Load()
}

// save re-sorts the document and writes it back.
//
// The sort happens here rather than in each subcommand because the ordering is
// a property of the artifact, not of the command that touched it: a duty added
// today and one deferred today end up in the same place either way.
func (a *app) save(doc *mdstore.Document) error {
	doc.Sort(a.now, a.cfg)
	return a.store.Save(doc, a.now)
}

// persona resolves the configured voice.
func (a *app) persona() (voice.Persona, error) {
	return voice.Get(a.cfg.Persona)
}

func (a *app) printf(format string, args ...interface{}) {
	fmt.Fprintf(a.out, format, args...)
}

// commandUsage is one subcommand's help. It walks the flag set rather than
// calling PrintDefaults so that `--today` stays out of the listing.
func commandUsage(w io.Writer, c command, fs *flag.FlagSet) {
	fmt.Fprintf(w, "secretary %s — %s\n\nFlags:\n", c.name(), c.summary())
	fs.VisitAll(func(f *flag.Flag) {
		if f.Usage == "" {
			return
		}
		fmt.Fprintf(w, "  --%-10s %s\n", f.Name, f.Usage)
	})
}

// usage is the help text. It lists the documented flags only.
func usage(w io.Writer) {
	fmt.Fprint(w, `secretary — a secretary who remembers your duties.

Usage:
  secretary [global flags] <command> [command flags]

Commands:
`)
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-9s %s\n", c.name(), c.summary())
	}
	fmt.Fprint(w, `
Global flags:
  --config PATH     path to secretary.yml (default secretary.yml)
  --artifact PATH   path to the artifact (default from the configuration)
`)
}
