// Package fixture generates a family tree of invented people.
//
// The demo and the tests use only this generator. All names, places, sources
// and stories are made up. The output is deterministic for a given seed.
package fixture

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/Renset/heirfold/internal/model"
)

// Options controls the generator.
type Options struct {
	// Locale is "ru" or "en".
	Locale string
	// Seed makes the output repeatable.
	Seed uint64
	// Year is the current year of the invented world.
	Year int
}

// DefaultOptions returns the options of the public demo.
func DefaultOptions(locale string) Options {
	return Options{Locale: locale, Seed: 20261109, Year: 2026}
}

const fixedChange = 1_780_000_000

type generator struct {
	r       *rand.Rand
	lang    *language
	tree    *model.Tree
	year    int
	seq     int
	counter map[string]int
	places  []string
	region  string
	sources []string
	tags    []string
}

func (g *generator) handle() string {
	g.seq++
	return fmt.Sprintf("_fx%020x", g.seq)
}

func (g *generator) id(prefix string) string {
	g.counter[prefix]++
	return fmt.Sprintf("%s%04d", prefix, g.counter[prefix])
}

func (g *generator) object(prefix string) model.Object {
	return model.Object{Handle: g.handle(), ID: g.id(prefix), Change: fixedChange}
}

func (g *generator) chance(p float64) bool { return g.r.Float64() < p }

func (g *generator) between(low, high int) int {
	if high <= low {
		return low
	}
	return low + g.r.IntN(high-low+1)
}

// Generate returns a new invented tree.
func Generate(o Options) *model.Tree {
	if o.Year == 0 {
		o.Year = DefaultOptions(o.Locale).Year
	}
	g := &generator{
		// A simple seeded generator is right here: this is demo data, not a secret.
		r:       rand.New(rand.NewPCG(o.Seed, 0x48454952)), // #nosec G404
		lang:    languageFor(o.Locale),
		year:    o.Year,
		counter: map[string]int{},
		tree: &model.Tree{
			Namespace: "http://gramps-project.org/xml/1.7.2/",
			Header:    model.Header{Created: model.Created{Date: "2026-11-09", Version: "6.0.8"}},
		},
	}
	g.tree.Header.Researcher = &model.Researcher{Name: g.lang.researcher}
	g.world()
	g.populate()
	return g.tree
}

// world creates the invented places, sources and tags.
func (g *generator) world() {
	region := model.Place{Object: g.object("P"), Type: "State", Title: g.lang.region, Names: []model.PlaceName{{Value: g.lang.region}}}
	g.tree.Places = append(g.tree.Places, region)
	g.region = region.Handle
	for _, def := range g.lang.places {
		title := def.name + ", " + g.lang.region
		place := model.Place{
			Object: g.object("P"), Type: def.kind, Title: title,
			Names:     []model.PlaceName{{Value: def.name}},
			Coord:     &model.Coord{Lat: fmt.Sprintf("%.4f", def.lat), Long: fmt.Sprintf("%.4f", def.long)},
			PlaceRefs: []model.PlaceRef{{HLink: region.Handle}},
		}
		g.tree.Places = append(g.tree.Places, place)
		g.places = append(g.places, place.Handle)
	}
	repositories := make([]string, 0, len(g.lang.repositories))
	for _, name := range g.lang.repositories {
		repo := model.Repository{Object: g.object("R"), Name: name, Type: "Archive"}
		g.tree.Repositories = append(g.tree.Repositories, repo)
		repositories = append(repositories, repo.Handle)
	}
	for i, def := range g.lang.sources {
		source := model.Source{
			Object: g.object("S"), Title: def.title, Author: def.author, PubInfo: def.pubinfo,
			RepoRefs: []model.RepoRef{{HLink: repositories[i%len(repositories)], CallNo: fmt.Sprintf("F-%d/%d", 11+i*7, 3+i), Medium: "Book"}},
		}
		g.tree.Sources = append(g.tree.Sources, source)
		g.sources = append(g.sources, source.Handle)
	}
	for i, tag := range g.lang.tags {
		item := model.Tag{Handle: g.handle(), Name: tag.name, Color: tag.color, Priority: i, Change: fixedChange}
		g.tree.Tags = append(g.tree.Tags, item)
		g.tags = append(g.tags, item.Handle)
	}
}

type personRef struct {
	index  int
	birth  int
	given  givenName
	family surnameForms
	male   bool
}

type couple struct {
	family     int
	father     personRef
	mother     personRef
	married    int
	generation int
}

// couplesPerGeneration limits the lines that continue in each generation, so
// that the tree reaches the present day within the size limit.
var couplesPerGeneration = []int{3, 4, 5, 6, 7, 7, 6, 3}

func (g *generator) populate() {
	var current []couple
	for i := 0; i < couplesPerGeneration[0]; i++ {
		base := g.between(1826, 1838)
		father := g.newPerson(true, base, g.pickSurname(), nil, false)
		mother := g.newPerson(false, base+g.between(1, 6), g.pickSurname(), nil, false)
		current = append(current, g.marry(father, mother, 0))
	}
	var candidates []personRef
	for generation := 0; len(current) > 0; generation++ {
		var eligible []personRef
		var leaders []personRef
		for _, parents := range current {
			children := g.children(parents)
			for i, child := range children {
				if child.birth > g.year-21 {
					continue
				}
				if i == 0 {
					leaders = append(leaders, child)
				} else {
					eligible = append(eligible, child)
				}
			}
			if generation >= 4 {
				candidates = append(candidates, children...)
			}
		}
		slots := 0
		if generation+1 < len(couplesPerGeneration) {
			slots = couplesPerGeneration[generation+1]
		}
		g.r.Shuffle(len(eligible), func(i, j int) { eligible[i], eligible[j] = eligible[j], eligible[i] })
		chosen := append(leaders, eligible...)
		if len(chosen) > slots {
			chosen = chosen[:slots]
		}
		var next []couple
		for _, child := range chosen {
			spouseYear := child.birth + g.between(-4, 5)
			spouse := g.newPerson(!child.male, spouseYear, g.pickSurname(), nil, false)
			// Spouses from the middle generations get parents of their own,
			// so the charts show both sides of a family.
			if generation >= 1 && g.chance(0.6) {
				depth := 1
				if generation >= 4 {
					depth = 2
				}
				g.addParents(spouse, depth)
			}
			if child.male {
				next = append(next, g.marry(child, spouse, generation+1))
			} else {
				next = append(next, g.marry(spouse, child, generation+1))
			}
		}
		current = next
	}
	// The home person is someone born near 1985, with a long ancestry.
	best := -1
	bestGap := 1 << 30
	for _, c := range candidates {
		gap := c.birth - 1985
		if gap < 0 {
			gap = -gap
		}
		if gap < bestGap && g.isAlive(c.index) {
			best, bestGap = c.index, gap
		}
	}
	if best >= 0 {
		g.tree.HomeHandle = g.tree.People[best].Handle
	}
}

func (g *generator) isAlive(index int) bool {
	for _, ref := range g.tree.People[index].EventRefs {
		if strings.HasPrefix(ref.Role, "Primary") {
			for i := range g.tree.Events {
				if g.tree.Events[i].Handle == ref.HLink && g.tree.Events[i].Type == "Death" {
					return false
				}
			}
		}
	}
	return true
}

func (g *generator) pickSurname() surnameForms {
	return g.lang.surnames[g.r.IntN(len(g.lang.surnames))]
}

// addParents gives a person a set of invented parents and grandparents.
func (g *generator) addParents(child personRef, depth int) {
	if depth == 0 || child.birth < 1800 {
		return
	}
	age := g.between(24, 36)
	fatherGiven := g.lang.male[g.r.IntN(len(g.lang.male))]
	father := g.newPersonNamed(true, child.birth-age, child.family, nil, fatherGiven, true)
	mother := g.newPerson(false, child.birth-age+g.between(0, 6), g.pickSurname(), nil, false)
	fam := g.marry(father, mother, -1)
	g.linkChild(fam.family, child.index)
	if depth > 1 {
		g.addParents(father, depth-1)
		if g.chance(0.5) {
			g.addParents(mother, depth-1)
		}
	}
}

func (g *generator) linkChild(family, child int) {
	f := &g.tree.Families[family]
	f.ChildRefs = append(f.ChildRefs, model.ChildRef{HLink: g.tree.People[child].Handle})
	p := &g.tree.People[child]
	p.ChildOf = append(p.ChildOf, model.Ref{HLink: f.Handle})
	// A patronymic follows the father, so it is set once the father is known.
	if f.Father != nil {
		for i := range g.tree.People {
			if g.tree.People[i].Handle == f.Father.HLink {
				g.applyPatronymic(child, g.tree.People[i].PrimaryName().First)
			}
		}
	}
}

func (g *generator) applyPatronymic(child int, fatherGiven string) {
	if g.lang.code != "ru" {
		return
	}
	given := givenName{}
	for _, name := range g.lang.male {
		if name.name == fatherGiven {
			given = name
		}
	}
	if given.name == "" {
		return
	}
	person := &g.tree.People[child]
	for i := range person.Names {
		name := &person.Names[i]
		patronymic := given.patronymicF
		if person.Gender == "M" {
			patronymic = given.patronymicM
		}
		found := false
		for j := range name.Surnames {
			if name.Surnames[j].Derivation == "Patronymic" {
				name.Surnames[j].Value = patronymic
				found = true
			}
		}
		if !found {
			no := model.Flag(false)
			name.Surnames = append(name.Surnames, model.Surname{Value: patronymic, Derivation: "Patronymic", Primary: &no})
		}
	}
}

func (g *generator) marry(father, mother personRef, generation int) couple {
	year := max(father.birth, mother.birth) + g.between(20, 28)
	family := model.Family{Object: g.object("F"), Rel: &model.FamilyRel{Type: "Married"}}
	family.Father = &model.Ref{HLink: g.tree.People[father.index].Handle}
	family.Mother = &model.Ref{HLink: g.tree.People[mother.index].Handle}
	if year < g.year {
		event := g.addEvent("Marriage", year, g.anyPlace(), "", 0.2)
		family.EventRefs = append(family.EventRefs, model.EventRef{HLink: event, Role: "Family"})
	}
	if generation >= 0 && g.chance(0.12) {
		note := g.addNote(g.lang.coupleStories[g.r.IntN(len(g.lang.coupleStories))], false)
		family.Notes = append(family.Notes, model.Ref{HLink: note})
	}
	g.tree.Families = append(g.tree.Families, family)
	index := len(g.tree.Families) - 1
	handle := g.tree.Families[index].Handle
	g.tree.People[father.index].ParentIn = append(g.tree.People[father.index].ParentIn, model.Ref{HLink: handle})
	g.tree.People[mother.index].ParentIn = append(g.tree.People[mother.index].ParentIn, model.Ref{HLink: handle})
	// The married name of the wife is an alternative name.
	if g.lang.code == "ru" || g.chance(0.5) {
		wife := &g.tree.People[mother.index]
		if len(wife.Names) > 0 && g.chance(0.7) {
			alt := model.Name{Alt: true, Type: "Married Name", First: wife.Names[0].First, Surnames: []model.Surname{{Value: father.family.female}}}
			wife.Names = append(wife.Names, alt)
		}
	}
	return couple{family: index, father: father, mother: mother, married: year, generation: generation}
}

func (g *generator) children(c couple) []personRef {
	var count int
	switch {
	case c.generation < 0:
		count = 1
	case c.generation < 2:
		count = g.between(3, 5)
	case c.generation < 5:
		count = g.between(2, 4)
	default:
		count = g.between(1, 3)
	}
	var out []personRef
	year := c.married + g.between(1, 3)
	for i := 0; i < count; i++ {
		if year > g.year-1 || year-c.mother.birth > 45 {
			break
		}
		male := g.chance(0.5)
		child := g.newPerson(male, year, c.father.family, &c.father, false)
		g.linkChild(c.family, child.index)
		out = append(out, child)
		year += g.between(2, 5)
	}
	return out
}

func (g *generator) newPerson(male bool, birth int, family surnameForms, father *personRef, namedLater bool) personRef {
	var given givenName
	if male {
		given = g.lang.male[g.r.IntN(len(g.lang.male))]
	} else {
		given = givenName{name: g.lang.female[g.r.IntN(len(g.lang.female))]}
	}
	return g.newPersonNamed(male, birth, family, father, given, namedLater)
}

func (g *generator) newPersonNamed(male bool, birth int, family surnameForms, father *personRef, given givenName, _ bool) personRef {
	gender := "F"
	surname := family.female
	if male {
		gender = "M"
		surname = family.male
	}
	name := model.Name{Type: "Birth Name", First: given.name, Surnames: []model.Surname{{Value: surname, Derivation: "Inherited"}}}
	if g.lang.code == "ru" && father != nil {
		patronymic := father.given.patronymicF
		if male {
			patronymic = father.given.patronymicM
		}
		if patronymic != "" {
			no := model.Flag(false)
			name.Surnames = append(name.Surnames, model.Surname{Value: patronymic, Derivation: "Patronymic", Primary: &no})
		}
	}
	if g.chance(0.05) {
		name.Nick = g.lang.nicknames[g.r.IntN(len(g.lang.nicknames))]
	}
	person := model.Person{Object: g.object("I"), Gender: gender, Names: []model.Name{name}}
	if g.chance(0.04) {
		person.Private = true
	}
	g.tree.People = append(g.tree.People, person)
	index := len(g.tree.People) - 1
	g.lifeEvents(index, birth, male)
	return personRef{index: index, birth: birth, given: given, family: family, male: male}
}

// lifeEvents adds the events, notes and evidence of one person.
func (g *generator) lifeEvents(index, birth int, male bool) {
	knownBirth := birth > 1890 || !g.chance(0.12)
	place := g.anyPlace()
	var refs []model.EventRef
	if knownBirth {
		handle := g.addEvent("Birth", birth, place, "", 0.15)
		refs = append(refs, model.EventRef{HLink: handle, Role: "Primary"})
	}
	if birth < 1918 && g.chance(0.45) {
		handle := g.addEvent("Baptism", birth, place, "", 0.3)
		refs = append(refs, model.EventRef{HLink: handle, Role: "Primary"})
	}
	if birth < g.year-20 && g.chance(0.75) {
		occupations := g.lang.occupationsF
		if male {
			occupations = g.lang.occupationsM
		}
		work := occupations[g.r.IntN(len(occupations))]
		handle := g.addEvent("Occupation", birth+g.between(18, 30), g.anyPlace(), work, 0.5)
		refs = append(refs, model.EventRef{HLink: handle, Role: "Primary"})
	}
	if birth < g.year-25 && g.chance(0.35) {
		handle := g.addEvent("Residence", birth+g.between(20, 45), g.anyPlace(), "", 0.5)
		event := g.findEvent(handle)
		event.Private = model.Flag(g.chance(0.25))
		refs = append(refs, model.EventRef{HLink: handle, Role: "Primary"})
	}
	if year, dead := g.deathYear(birth); dead {
		handle := g.addEvent("Death", year, g.anyPlace(), "", 0.15)
		refs = append(refs, model.EventRef{HLink: handle, Role: "Primary"})
		if g.chance(0.4) {
			burial := g.addEvent("Burial", year, g.anyPlace(), "", 0.2)
			refs = append(refs, model.EventRef{HLink: burial, Role: "Primary"})
		}
	}
	person := &g.tree.People[index]
	person.EventRefs = refs
	if g.chance(0.22) {
		stories := g.lang.storiesF
		if male {
			stories = g.lang.storiesM
		}
		text := strings.ReplaceAll(stories[g.r.IntN(len(stories))], "{place}", g.lang.places[g.r.IntN(len(g.lang.places))].name)
		note := g.addNote(text, false)
		person.Notes = append(person.Notes, model.Ref{HLink: note})
	}
	if g.chance(0.08) {
		note := g.addNote(g.lang.privateNote, true)
		person.Notes = append(person.Notes, model.Ref{HLink: note})
	}
	if g.chance(0.18) {
		person.Attributes = append(person.Attributes, model.Attribute{Type: g.lang.hobbyType, Value: g.lang.hobbies[g.r.IntN(len(g.lang.hobbies))]})
	}
	if g.chance(0.1) {
		person.Tags = append(person.Tags, model.Ref{HLink: g.tags[g.r.IntN(len(g.tags))]})
	}
	if g.chance(0.35) {
		person.Citations = append(person.Citations, model.Ref{HLink: g.addCitation()})
	}
	if index > 8 && g.chance(0.05) {
		other := g.tree.People[g.r.IntN(index)]
		person.PersonRefs = append(person.PersonRefs, model.PersonRef{HLink: other.Handle, Rel: g.lang.godparent})
	}
}

func (g *generator) findEvent(handle string) *model.Event {
	for i := len(g.tree.Events) - 1; i >= 0; i-- {
		if g.tree.Events[i].Handle == handle {
			return &g.tree.Events[i]
		}
	}
	return nil
}

func (g *generator) deathYear(birth int) (int, bool) {
	var probability float64
	switch {
	case birth < 1925:
		probability = 1
	case birth < 1955:
		probability = 0.6
	default:
		probability = 0.03
	}
	if !g.chance(probability) {
		return 0, false
	}
	age := g.between(38, 93)
	year := birth + age
	if year > g.year {
		if birth < 1925 {
			return g.year - g.between(1, 5), true
		}
		return 0, false
	}
	return year, true
}

func (g *generator) anyPlace() string {
	return g.places[g.r.IntN(len(g.places))]
}

// addEvent creates an event. vagueness is the chance that the date is not exact.
func (g *generator) addEvent(kind string, year int, place, description string, vagueness float64) string {
	event := model.Event{Object: g.object("E"), Type: kind, Description: description, Place: &model.Ref{HLink: place}}
	month, day := g.between(1, 12), g.between(1, 28)
	val := fmt.Sprintf("%04d-%02d-%02d", year, month, day)
	calendar := ""
	if g.lang.code == "ru" && year < 1918 && g.chance(0.4) {
		calendar = "Julian"
	}
	switch {
	case g.chance(vagueness / 3):
		event.DateVal = &model.DateVal{Val: fmt.Sprintf("%04d", year), Type: "about", CFormat: calendar}
	case g.chance(vagueness / 3):
		event.DateVal = &model.DateVal{Val: fmt.Sprintf("%04d-%02d", year, month), CFormat: calendar}
	case g.chance(vagueness / 3):
		event.DateRange = &model.DatePair{Start: fmt.Sprintf("%04d", year), Stop: fmt.Sprintf("%04d", year+g.between(1, 3)), CFormat: calendar}
	default:
		event.DateVal = &model.DateVal{Val: val, CFormat: calendar}
	}
	if g.chance(0.35) {
		event.Citations = append(event.Citations, model.Ref{HLink: g.addCitation()})
	}
	g.tree.Events = append(g.tree.Events, event)
	return event.Handle
}

func (g *generator) addNote(text string, private bool) string {
	note := model.Note{Object: g.object("N"), Type: "Person Note", Text: text}
	note.Private = model.Flag(private)
	g.tree.Notes = append(g.tree.Notes, note)
	return note.Handle
}

func (g *generator) addCitation() string {
	citation := model.Citation{
		Object: g.object("C"), Page: fmt.Sprintf(g.lang.pageFormat, g.between(3, 412)), Confidence: g.between(2, 4),
		Source: model.Ref{HLink: g.sources[g.r.IntN(len(g.sources))]},
	}
	g.tree.Citations = append(g.tree.Citations, citation)
	return citation.Handle
}
