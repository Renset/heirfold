package privacy

import (
	"strconv"
	"testing"

	"github.com/Renset/heirfold/internal/model"
)

const testYear = 2026

type builder struct {
	tree *model.Tree
	next int
}

func newBuilder() *builder { return &builder{tree: &model.Tree{}} }

func (b *builder) handle() string {
	b.next++
	return "_h" + strconv.Itoa(b.next)
}

func (b *builder) person(events ...model.EventRef) string {
	handle := b.handle()
	b.tree.People = append(b.tree.People, model.Person{Object: model.Object{Handle: handle, ID: handle}, EventRefs: events})
	return handle
}

func (b *builder) event(kind string, dated model.Dated) model.EventRef {
	handle := b.handle()
	b.tree.Events = append(b.tree.Events, model.Event{Object: model.Object{Handle: handle}, Type: kind, Dated: dated})
	return model.EventRef{HLink: handle, Role: "Primary"}
}

func (b *builder) family(father, mother string, children ...string) {
	family := model.Family{Object: model.Object{Handle: b.handle()}}
	if father != "" {
		family.Father = &model.Ref{HLink: father}
	}
	if mother != "" {
		family.Mother = &model.Ref{HLink: mother}
	}
	for _, child := range children {
		family.ChildRefs = append(family.ChildRefs, model.ChildRef{HLink: child})
	}
	b.tree.Families = append(b.tree.Families, family)
}

func year(y int) model.Dated {
	return model.Dated{DateVal: &model.DateVal{Val: strconv.Itoa(y)}}
}

func TestLivingOwnEvidence(t *testing.T) {
	cases := []struct {
		name string
		make func(b *builder) string
		want bool
	}{
		{"no data at all", func(b *builder) string { return b.person() }, true},
		{"born recently", func(b *builder) string { return b.person(b.event("Birth", year(1990))) }, true},
		{"born 110 years ago is still living", func(b *builder) string { return b.person(b.event("Birth", year(testYear-110))) }, true},
		{"born 111 years ago", func(b *builder) string { return b.person(b.event("Birth", year(testYear-111))) }, false},
		{"death event without date", func(b *builder) string { return b.person(b.event("Death", model.Dated{})) }, false},
		{"burial proves death", func(b *builder) string { return b.person(b.event("Burial", year(1950))) }, false},
		{"unreadable birth date", func(b *builder) string {
			return b.person(b.event("Birth", model.Dated{DateStr: &model.DateStr{Val: "long ago"}}))
		}, true},
		{"a will does not prove a death", func(b *builder) string {
			return b.person(b.event("Birth", year(1990)), b.event("Will", year(2024)))
		}, true},
		{"after a year has no upper bound", func(b *builder) string {
			return b.person(b.event("Birth", model.Dated{DateVal: &model.DateVal{Val: "1895", Type: "after"}}))
		}, true},
		{"open range has no upper bound", func(b *builder) string {
			return b.person(b.event("Birth", model.Dated{DateRange: &model.DatePair{Start: "1880", Stop: "????"}}))
		}, true},
		{"witness at a funeral is alive", func(b *builder) string {
			ref := b.event("Death", year(2001))
			ref.Role = "Witness"
			return b.person(ref)
		}, true},
		{"about 1900 stays within the margin", func(b *builder) string {
			return b.person(b.event("Birth", model.Dated{DateVal: &model.DateVal{Val: strconv.Itoa(testYear - 113), Type: "about"}}))
		}, true},
		{"after a date widens the upper bound", func(b *builder) string {
			return b.person(b.event("Birth", model.Dated{DateVal: &model.DateVal{Val: strconv.Itoa(testYear - 125), Type: "after"}}))
		}, true},
		{"range uses the end", func(b *builder) string {
			return b.person(b.event("Birth", model.Dated{DateRange: &model.DatePair{Start: "1800", Stop: strconv.Itoa(testYear - 50)}}))
		}, true},
		{"julian date", func(b *builder) string {
			return b.person(b.event("Birth", model.Dated{DateVal: &model.DateVal{Val: "1850-01-01", CFormat: "Julian"}}))
		}, false},
		{"hebrew date is converted", func(b *builder) string {
			return b.person(b.event("Birth", model.Dated{DateVal: &model.DateVal{Val: "5780", CFormat: "Hebrew"}}))
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBuilder()
			person := tc.make(b)
			if got := Living(b.tree, testYear)[person]; got != tc.want {
				t.Fatalf("living = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLivingThroughRelatives(t *testing.T) {
	t.Run("parent of an old person is dead", func(t *testing.T) {
		b := newBuilder()
		parent := b.person()
		child := b.person(b.event("Birth", year(1880)))
		b.family(parent, "", child)
		if Living(b.tree, testYear)[parent] {
			t.Fatal("parent must be dead")
		}
	})
	t.Run("parent of a young person stays living", func(t *testing.T) {
		b := newBuilder()
		parent := b.person()
		child := b.person(b.event("Birth", year(1990)))
		b.family(parent, "", child)
		if !Living(b.tree, testYear)[parent] {
			t.Fatal("parent must stay living")
		}
	})
	t.Run("child of a very old person is dead", func(t *testing.T) {
		b := newBuilder()
		parent := b.person(b.event("Birth", year(1800)))
		child := b.person()
		b.family(parent, "", child)
		if Living(b.tree, testYear)[child] {
			t.Fatal("child must be dead")
		}
	})
	t.Run("child of a person born in 1900 stays living", func(t *testing.T) {
		b := newBuilder()
		parent := b.person(b.event("Birth", year(1900)))
		child := b.person()
		b.family(parent, "", child)
		if !Living(b.tree, testYear)[child] {
			t.Fatal("child must stay living")
		}
	})
	t.Run("spouse bound", func(t *testing.T) {
		b := newBuilder()
		husband := b.person(b.event("Birth", year(1800)))
		wife := b.person()
		b.family(husband, wife)
		if Living(b.tree, testYear)[wife] {
			t.Fatal("wife must be dead")
		}
	})
	t.Run("evidence crosses generations", func(t *testing.T) {
		b := newBuilder()
		grandparent := b.person()
		parent := b.person()
		child := b.person(b.event("Birth", year(1905)))
		b.family(grandparent, "", parent)
		b.family(parent, "", child)
		got := Living(b.tree, testYear)
		if got[grandparent] || got[parent] {
			t.Fatalf("ancestors must be dead: %v", got)
		}
	})
	t.Run("a link to a missing person gives no evidence", func(t *testing.T) {
		b := newBuilder()
		wife := b.person(b.event("Birth", year(1990)))
		b.family("_missing", wife)
		husband := b.person()
		b.family(husband, "_missing")
		got := Living(b.tree, testYear)
		if !got[wife] || !got[husband] {
			t.Fatalf("both must stay living: %v", got)
		}
	})
	t.Run("own date beats a mistyped relative", func(t *testing.T) {
		b := newBuilder()
		father := b.person(b.event("Birth", year(1960)))
		mother := b.person(b.event("Birth", year(1962)))
		child := b.person(b.event("Birth", year(1890)))
		b.family(father, mother, child)
		got := Living(b.tree, testYear)
		if !got[father] || !got[mother] {
			t.Fatalf("parents must stay living: %v", got)
		}
	})
	t.Run("a bad date in a cycle does not spread to people with own dates", func(t *testing.T) {
		b := newBuilder()
		a := b.person(b.event("Birth", year(1990)))
		c := b.person(b.event("Birth", year(1850)))
		b.family(a, "", c)
		b.family(c, "", a)
		if !Living(b.tree, testYear)[a] {
			t.Fatal("a person born in 1990 must stay living")
		}
	})
	t.Run("a loop of undated people gives no evidence", func(t *testing.T) {
		b := newBuilder()
		x := b.person()
		y := b.person()
		child := b.person(b.event("Birth", year(1990)))
		b.family(x, "", y)
		b.family(y, "", x)
		b.family(x, "", child)
		got := Living(b.tree, testYear)
		if !got[x] || !got[y] {
			t.Fatalf("people in a loop must stay living: %v", got)
		}
	})
	t.Run("people outside a loop keep their evidence", func(t *testing.T) {
		b := newBuilder()
		x := b.person()
		y := b.person()
		b.family(x, "", y)
		b.family(y, "", x)
		parent := b.person()
		child := b.person(b.event("Birth", year(1880)))
		b.family(parent, "", child)
		if Living(b.tree, testYear)[parent] {
			t.Fatal("the parent must be dead")
		}
	})
	t.Run("cycle in bad data ends", func(t *testing.T) {
		b := newBuilder()
		a := b.person(b.event("Birth", year(1990)))
		c := b.person()
		b.family(a, "", c)
		b.family(c, "", a)
		Living(b.tree, testYear)
	})
}

func TestEventTypes(t *testing.T) {
	if !IsDeathType(" death ") || IsDeathType("Birth") {
		t.Fatal("IsDeathType is wrong")
	}
	if !IsBirthType("Baptism") || IsBirthType("Death") {
		t.Fatal("IsBirthType is wrong")
	}
}
