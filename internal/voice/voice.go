// Package voice turns computed facts about duties into something a person says.
//
// Nothing here decides anything: statuses come from schedule, batches from
// grouping. The package only supplies the wording, which is why the persona is
// an interface — a second butler is a new file, not a refactor.
//
// The wording is deterministic. A phrase is chosen from its variants by a hash
// of the facts it describes (usually the task id and the day), so Alfred says
// the same thing twice on the same day, a different thing tomorrow, and a test
// can assert on either without touching a clock or a random source.
package voice

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/bayanovea/ai-secretary/internal/grouping"
	"github.com/bayanovea/ai-secretary/internal/schedule"
	"github.com/bayanovea/ai-secretary/internal/task"
)

// ErrUnknownPersona is returned by Get for a name nobody implements.
var ErrUnknownPersona = fmt.Errorf("unknown persona")

// Persona is everything the briefing needs said out loud.
//
// Every method returns a complete sentence, ready to print: the caller decides
// what to report, the persona decides how it sounds.
type Persona interface {
	// Greeting opens the briefing on the given day.
	Greeting(now time.Time) string
	// Overdue reports a duty whose deadline passed days ago.
	Overdue(t task.Task, days int) string
	// StartNow reports a duty that has to be started today.
	StartNow(t task.Task) string
	// Soon reports a duty that has to be started in days days.
	Soon(t task.Task, days int) string
	// GroupIntro introduces a batch of duties worth doing in one go.
	GroupIntro(g grouping.Group) string
	// Complaint voices a flag raised about a duty.
	Complaint(f schedule.Flag, t task.Task) string
	// AllClear is what is said when there is nothing to report.
	AllClear() string
	// Signoff closes the briefing.
	Signoff() string
}

// personas is the registry the config's `persona:` field selects from.
var personas = map[string]func() Persona{
	"alfred": func() Persona { return Alfred{} },
}

// Get returns the persona registered under name. The name is matched without
// regard to case or surrounding space; anything unregistered is an error, so a
// typo in the config is reported rather than silently ignored.
func Get(name string) (Persona, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil, fmt.Errorf("%w: no name given (available: %s)", ErrUnknownPersona, strings.Join(Names(), ", "))
	}
	build, ok := personas[key]
	if !ok {
		return nil, fmt.Errorf("%w %q (available: %s)", ErrUnknownPersona, name, strings.Join(Names(), ", "))
	}
	return build(), nil
}

// Names lists the registered personas in a fixed order.
func Names() []string {
	names := make([]string, 0, len(personas))
	for name := range personas {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// pick chooses one of the variants by hashing the seed parts. The choice is a
// pure function of those parts: the same facts always produce the same phrase,
// and facts that change daily — the day itself, the number of days overdue —
// make the phrase change with them.
//
// FNV-1a is used because it is in the standard library, is stable across
// releases and platforms, and nothing here needs a cryptographic hash.
func pick(variants []string, seed ...string) string {
	if len(variants) == 0 {
		return ""
	}
	h := fnv.New32a()
	for i, part := range seed {
		if i > 0 {
			// A separator keeps ("ab","c") apart from ("a","bc").
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte(part))
	}
	return variants[int(h.Sum32()%uint32(len(variants)))]
}

// dayKey renders a date as a seed part, dropping the clock and the zone so that
// two `now` values on the same calendar day pick the same phrase.
func dayKey(t time.Time) string {
	return t.Format("2006-01-02")
}
