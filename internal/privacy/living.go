// Package privacy decides who is treated as living and what a viewer can see.
package privacy

import (
	"strings"

	"github.com/Renset/heirfold/internal/model"
)

// The numbers below follow one rule: when in doubt, a person is living.
// A mistake then hides a dead person, and never shows a living person.
const (
	// MaxAge is the largest age that Heirfold accepts for a living person.
	MaxAge = 110
	// minParentAge is the smallest age at which a person can become a parent.
	minParentAge = 12
	// maxParentAge is the largest age at which a person can become a parent.
	maxParentAge = 80
	// maxSpouseGap is the largest age difference within a couple.
	maxSpouseGap = 40
	// relaxRounds limits how far evidence spreads through the tree.
	relaxRounds = 12
)

// deathTypes lists the event types that prove a death.
var deathTypes = map[string]struct{}{
	"death":     {},
	"burial":    {},
	"cremation": {},
	"probate":   {},
}

// birthTypes lists the event types that bound the birth year.
var birthTypes = map[string]struct{}{
	"birth":       {},
	"baptism":     {},
	"christening": {},
}

// IsDeathType reports whether an event type proves a death.
func IsDeathType(t string) bool {
	_, ok := deathTypes[strings.ToLower(strings.TrimSpace(t))]
	return ok
}

// IsBirthType reports whether an event type bounds the birth year.
func IsBirthType(t string) bool {
	_, ok := birthTypes[strings.ToLower(strings.TrimSpace(t))]
	return ok
}

// Living computes the set of people that Heirfold treats as living in the
// given year. The function is deliberately conservative:
//
//   - A death-type event of the person proves a death, also without a date.
//   - A person with a birth year more than MaxAge years ago is dead.
//   - A person's own birth date always beats a bound from relatives, because
//     one mistyped date of a relative must not reveal a living person.
//   - Without own dates, the function bounds the birth year through parents,
//     children and spouses, and uses only bounds that are certain.
//   - All other people are living.
//
// Only events with the role Primary (or no role) count, so a witness at a
// funeral is not dead.
func Living(t *model.Tree, year int) map[string]bool {
	events := make(map[string]*model.Event, len(t.Events))
	for i := range t.Events {
		events[t.Events[i].Handle] = &t.Events[i]
	}

	// latest is an upper bound on the birth year of each person.
	latest := make(map[string]int, len(t.People))
	hasOwnDate := make(map[string]bool, len(t.People))
	dead := make(map[string]bool, len(t.People))
	const unknown = 1 << 30

	for i := range t.People {
		person := &t.People[i]
		bound := unknown
		for _, ref := range person.EventRefs {
			if ref.Role != "" && !strings.EqualFold(ref.Role, "Primary") {
				continue
			}
			event := events[ref.HLink]
			if event == nil {
				continue
			}
			if IsDeathType(event.Type) {
				dead[person.Handle] = true
			}
			if !IsBirthType(event.Type) {
				continue
			}
			if y, ok := event.View().LatestYear(); ok && y < bound {
				bound = y
			}
		}
		latest[person.Handle] = bound
		hasOwnDate[person.Handle] = bound != unknown
	}

	// A loop of parent and child links is bad data, and each round through the
	// loop would push the bound lower. People in a loop give no evidence and
	// take none.
	loop := parentLoops(t)

	// Spread the bounds through the family links. A bound moves only in the
	// safe direction: each step adds the largest gap that is possible.
	relax := func(target string, value int) bool {
		current, ok := latest[target]
		if !ok || hasOwnDate[target] || loop[target] || value >= current {
			return false
		}
		latest[target] = value
		return true
	}
	// usable returns the bound of a person if that bound is evidence.
	usable := func(handle string) (int, bool) {
		bound, ok := latest[handle]
		return bound, ok && bound != unknown && !loop[handle]
	}
	for round := 0; round < relaxRounds; round++ {
		changed := false
		for i := range t.Families {
			family := &t.Families[i]
			var parents []string
			if family.Father != nil {
				parents = append(parents, family.Father.HLink)
			}
			if family.Mother != nil {
				parents = append(parents, family.Mother.HLink)
			}
			// A parent is born at least minParentAge years before any child.
			for _, child := range family.ChildRefs {
				bound, ok := usable(child.HLink)
				if !ok {
					continue
				}
				for _, parent := range parents {
					changed = relax(parent, bound-minParentAge) || changed
				}
			}
			// A child is born at most maxParentAge years after a parent.
			for _, parent := range parents {
				bound, ok := usable(parent)
				if !ok {
					continue
				}
				for _, child := range family.ChildRefs {
					changed = relax(child.HLink, bound+maxParentAge) || changed
				}
			}
			// Spouses differ by at most maxSpouseGap years.
			if len(parents) == 2 {
				// A link to a missing person gives no evidence.
				if bound, ok := usable(parents[0]); ok {
					changed = relax(parents[1], bound+maxSpouseGap) || changed
				}
				if bound, ok := usable(parents[1]); ok {
					changed = relax(parents[0], bound+maxSpouseGap) || changed
				}
			}
		}
		if !changed {
			break
		}
	}

	result := make(map[string]bool, len(t.People))
	for i := range t.People {
		handle := t.People[i].Handle
		if dead[handle] {
			result[handle] = false
			continue
		}
		bound := latest[handle]
		result[handle] = bound == unknown || year-bound <= MaxAge
	}
	return result
}

// parentLoops returns the people on a loop of parent and child links, or on a
// path between two loops. It removes every person without a parent left, then
// every person without a child left. Only such loops can remain.
func parentLoops(t *model.Tree) map[string]bool {
	children := make(map[string][]string)
	parents := make(map[string][]string)
	remaining := make(map[string]bool)
	for i := range t.Families {
		family := &t.Families[i]
		for _, parent := range []*model.Ref{family.Father, family.Mother} {
			if parent == nil {
				continue
			}
			for _, child := range family.ChildRefs {
				children[parent.HLink] = append(children[parent.HLink], child.HLink)
				parents[child.HLink] = append(parents[child.HLink], parent.HLink)
				remaining[parent.HLink] = true
				remaining[child.HLink] = true
			}
		}
	}
	prune := func(forward, backward map[string][]string) {
		degree := make(map[string]int, len(remaining))
		var queue []string
		for person := range remaining {
			for _, other := range backward[person] {
				if remaining[other] {
					degree[person]++
				}
			}
			if degree[person] == 0 {
				queue = append(queue, person)
			}
		}
		for len(queue) > 0 {
			person := queue[0]
			queue = queue[1:]
			delete(remaining, person)
			for _, other := range forward[person] {
				if !remaining[other] {
					continue
				}
				degree[other]--
				if degree[other] == 0 {
					queue = append(queue, other)
				}
			}
		}
	}
	prune(children, parents)
	prune(parents, children)
	return remaining
}
