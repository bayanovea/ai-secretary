// Package grouping proposes batches of duties that are convenient to do in one
// go: one trip to town, one conversation with Helen, one evening spent on the
// move.
//
// The signals are ranked by how reliably they mean "the same errand". A shared
// context is a place or a tool, so it is nearly always right. A shared from is a
// person you will be talking to anyway. A shared label only says the duties are
// about the same subject, which is a weaker hint and therefore needs a bigger
// pile before it is worth mentioning.
//
// Nothing here reads a clock: the same tasks always produce the same groups,
// whatever order they arrive in.
package grouping

import (
	"fmt"
	"sort"

	"github.com/bayanovea/ai-secretary/internal/config"
	"github.com/bayanovea/ai-secretary/internal/task"
)

// Kind is the signal a group was built from.
type Kind int

// The three signals, in the order they are tried.
const (
	KindContext Kind = iota
	KindPerson
	KindLabel
)

// String returns the Russian label used in Alfred's speech.
func (k Kind) String() string {
	switch k {
	case KindContext:
		return "context"
	case KindPerson:
		return "from"
	case KindLabel:
		return "label"
	default:
		return "unknown"
	}
}

// How many duties a signal needs before a group is worth proposing.
const (
	// MinContextGroup: two errands in the same place already save a trip.
	MinContextGroup = 2
	// MinPersonGroup: two questions for the same person save a conversation.
	MinPersonGroup = 2
	// MinLabelGroup: a shared subject is a weaker hint, so the bar is higher.
	MinLabelGroup = 3
	// MinBackgroundGroup applies to a group made only of tier-3 duties: none of
	// them is urgent, so it takes a bigger pile to be worth interrupting for.
	MinBackgroundGroup = 3
)

// Group is a set of duties that share one reason to be done together.
type Group struct {
	Kind  Kind
	Key   string
	Tasks []task.Task
	// Reason is the factual clause behind the group («same place: @town»).
	// The voice package turns it into something Alfred would actually say.
	Reason string
}

// Size reports how many duties are in the group.
func (g Group) Size() int { return len(g.Tasks) }

// TopTier is the importance of the most important duty in the group — the
// smallest tier number, since tier 1 is critical. It is what the group is
// sorted by, so a batch containing a critical duty is proposed first.
func (g Group) TopTier() int {
	top := task.MaxTier
	for _, t := range g.Tasks {
		if t.Tier < top {
			top = t.Tier
		}
	}
	return top
}

// Build returns the batches worth proposing, most important first.
//
// The signals are tried in order — context, then from, then label — and a duty
// is claimed by the first group it lands in, so nothing is suggested twice.
// Completed duties are ignored: they are not work any more.
//
// cfg is accepted for symmetry with the other logic packages and to keep the
// signature stable once grouping starts consulting the configured contexts; the
// grouping itself is a pure function of the duties.
func Build(tasks []task.Task, cfg *config.Config) []Group {
	if cfg == nil {
		cfg = config.Default()
	}

	claimed := make([]bool, len(tasks))
	var groups []Group
	for _, p := range passes() {
		groups = append(groups, p.build(tasks, claimed)...)
	}

	sortGroups(groups)
	return groups
}

// pass is one signal: how to read its key off a duty and how many duties it
// takes before the group is worth proposing.
type pass struct {
	kind   Kind
	min    int
	keys   func(task.Task) []string
	reason func(key string) string
}

// passes lists the signals in priority order.
func passes() []pass {
	return []pass{
		{
			kind: KindContext,
			min:  MinContextGroup,
			keys: func(t task.Task) []string { return []string{t.Context} },
			reason: func(key string) string {
				return fmt.Sprintf("same place: %s", key)
			},
		},
		{
			kind: KindPerson,
			min:  MinPersonGroup,
			keys: func(t task.Task) []string {
				// Duties you set yourself share no conversation, so `from:self`
				// is not a reason to batch anything.
				if t.From == task.SelfAssigned {
					return nil
				}
				return []string{t.From}
			},
			reason: func(key string) string {
				return fmt.Sprintf("one conversation: %s", key)
			},
		},
		{
			kind: KindLabel,
			min:  MinLabelGroup,
			keys: func(t task.Task) []string { return t.Labels },
			reason: func(key string) string {
				return fmt.Sprintf("same theme: %s", key)
			},
		},
	}
}

// build forms every group this signal supports, claiming the duties it uses.
//
// Keys are visited in sorted order, so a duty that could join two groups of the
// same kind — two labels, say — always joins the same one.
func (p pass) build(tasks []task.Task, claimed []bool) []Group {
	buckets := make(map[string][]int)
	for i, t := range tasks {
		if claimed[i] || t.Done {
			continue
		}
		for _, key := range p.keys(t) {
			if key == "" {
				continue
			}
			buckets[key] = append(buckets[key], i)
		}
	}

	var groups []Group
	for _, key := range sortedKeys(buckets) {
		var members []int
		for _, i := range buckets[key] {
			if claimed[i] {
				continue
			}
			members = append(members, i)
		}
		if !worthProposing(tasks, members, p.min) {
			continue
		}

		group := Group{Kind: p.kind, Key: key, Reason: p.reason(key)}
		for _, i := range members {
			claimed[i] = true
			group.Tasks = append(group.Tasks, tasks[i])
		}
		sortTasks(group.Tasks)
		groups = append(groups, group)
	}
	return groups
}

// worthProposing applies the signal's threshold, raised to MinBackgroundGroup
// when every duty in the batch is background.
func worthProposing(tasks []task.Task, members []int, min int) bool {
	if len(members) < min {
		return false
	}
	if len(members) >= MinBackgroundGroup {
		return true
	}
	for _, i := range members {
		if tasks[i].Tier != task.MaxTier {
			return true
		}
	}
	return false
}

// sortGroups orders the proposals: the batch containing the most important duty
// first, then the bigger batch, then by key and kind so the order is total.
func sortGroups(groups []Group) {
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if a.TopTier() != b.TopTier() {
			return a.TopTier() < b.TopTier()
		}
		if a.Size() != b.Size() {
			return a.Size() > b.Size()
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.Kind < b.Kind
	})
}

// sortTasks orders the duties inside a batch: most important first, then by id,
// so the batch reads the same however the artifact was ordered.
func sortTasks(tasks []task.Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Tier != tasks[j].Tier {
			return tasks[i].Tier < tasks[j].Tier
		}
		return tasks[i].ID < tasks[j].ID
	})
}

// sortedKeys returns the bucket keys in a fixed order.
func sortedKeys(buckets map[string][]int) []string {
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
