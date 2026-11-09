package gramps

import (
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Renset/heirfold/internal/fixture"
	"github.com/Renset/heirfold/internal/model"
)

func parseBytes(t *testing.T, data []byte) *Result {
	t.Helper()
	res, err := Parse(bytes.NewReader(data), DefaultLimits)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return res
}

func TestRoundTripKeepsEveryField(t *testing.T) {
	for _, locale := range []string{"ru", "en"} {
		t.Run(locale, func(t *testing.T) {
			tree := fixture.Generate(fixture.DefaultOptions(locale))
			data, err := Marshal(tree)
			if err != nil {
				t.Fatal(err)
			}
			res := parseBytes(t, data)
			if res.WarningsTotal != 0 {
				t.Fatalf("unexpected warnings: %+v", res.Warnings)
			}
			if !reflect.DeepEqual(tree, res.Tree) {
				t.Fatal("the tree changed after a round trip")
			}
		})
	}
}

// TestRoundTripExample uses the example file of Gramps Desktop. The repository
// does not contain that file, so the test needs a path in the environment.
func TestRoundTripExample(t *testing.T) {
	path := os.Getenv("HEIRFOLD_EXAMPLE_GRAMPS")
	if path == "" {
		t.Skip("HEIRFOLD_EXAMPLE_GRAMPS is not set")
	}
	data, err := os.ReadFile(path) // #nosec G304,G703 -- test input from the developer.
	if err != nil {
		t.Fatal(err)
	}
	first := parseBytes(t, data)
	if len(first.Tree.People) == 0 {
		t.Fatal("no people")
	}
	out, err := Marshal(first.Tree)
	if err != nil {
		t.Fatal(err)
	}
	second := parseBytes(t, out)
	if !reflect.DeepEqual(first.Tree, second.Tree) {
		t.Fatal("the example changed after a round trip")
	}
}

func TestReadsGzip(t *testing.T) {
	data, err := Marshal(fixture.Generate(fixture.DefaultOptions("en")))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if res := parseBytes(t, buf.Bytes()); len(res.Tree.People) == 0 {
		t.Fatal("no people")
	}
}

func TestRejectsOtherFiles(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"plain text":   "hello",
		"other xml":    `<?xml version="1.0"?><html><body/></html>`,
		"no namespace": `<database><people/></database>`,
		"other ns":     `<database xmlns="http://example.org/"><people/></database>`,
		"broken":       `<database xmlns="http://gramps-project.org/xml/1.7.2/"><people>`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(input), DefaultLimits); !errors.Is(err, ErrNotGramps) {
				t.Fatalf("want ErrNotGramps, got %v", err)
			}
		})
	}
}

// TestEntitiesDoNotExpand covers XXE and the "billion laughs" attack.
func TestEntitiesDoNotExpand(t *testing.T) {
	cases := map[string]string{
		"external": `<?xml version="1.0"?><!DOCTYPE database [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>
<database xmlns="http://gramps-project.org/xml/1.7.2/"><notes><note handle="_a" change="1" id="N1" type="General"><text>&xxe;</text></note></notes></database>`,
		"billion laughs": `<?xml version="1.0"?><!DOCTYPE database [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;&a;&a;&a;&a;&a;">]>
<database xmlns="http://gramps-project.org/xml/1.7.2/"><notes><note handle="_a" change="1" id="N1" type="General"><text>&b;</text></note></notes></database>`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(input), DefaultLimits); !errors.Is(err, ErrNotGramps) {
				t.Fatalf("want ErrNotGramps, got %v", err)
			}
		})
	}
}

func TestDeepNestingIsRejected(t *testing.T) {
	const depth = 5000
	input := `<database xmlns="http://gramps-project.org/xml/1.7.2/">` +
		strings.Repeat("<a>", depth) + strings.Repeat("</a>", depth) + `</database>`
	if _, err := Parse(strings.NewReader(input), DefaultLimits); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("want ErrTooDeep, got %v", err)
	}
}

func TestDecompressionBomb(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(`<database xmlns="http://gramps-project.org/xml/1.7.2/"><notes><note handle="_a" change="1" id="N1" type="General"><text>`)); err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("a"), 1<<20)
	for i := 0; i < 32; i++ {
		if _, err := gz.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := Parse(&buf, Limits{MaxBytes: 1 << 20, MaxObjects: 100})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestObjectLimit(t *testing.T) {
	input := `<database xmlns="http://gramps-project.org/xml/1.7.2/"><tags>` +
		strings.Repeat(`<tag handle="_a" name="x" color="#000" priority="0" change="1"/>`, 5) + `</tags></database>`
	_, err := Parse(strings.NewReader(input), Limits{MaxBytes: 1 << 20, MaxObjects: 3})
	if !errors.Is(err, ErrTooManyObject) {
		t.Fatalf("want ErrTooManyObject, got %v", err)
	}
}

func TestWarnsAboutBrokenLinks(t *testing.T) {
	input := `<database xmlns="http://gramps-project.org/xml/1.7.2/"><header><created date="2026-01-01" version="6.0"/></header>
<people><person handle="_p1" change="1" id="I1"><gender>M</gender><childof hlink="_missing"/></person>
<person handle="_p2" change="1" id="I1"><gender>F</gender></person></people></database>`
	res := parseBytes(t, []byte(input))
	codes := map[string]int{}
	for _, w := range res.Warnings {
		codes[w.Code]++
	}
	if codes["broken_link"] != 1 || codes["duplicate_id"] != 1 {
		t.Fatalf("unexpected warnings: %+v", res.Warnings)
	}
}

func TestWarnsAboutOldFormat(t *testing.T) {
	input := `<database xmlns="http://gramps-project.org/xml/1.5.0/"><header><created date="2020-01-01" version="4.0"/></header></database>`
	res := parseBytes(t, []byte(input))
	if len(res.Warnings) != 1 || res.Warnings[0].Code != "old_format" {
		t.Fatalf("unexpected warnings: %+v", res.Warnings)
	}
}

func TestWarningsHoldNoNames(t *testing.T) {
	input := `<database xmlns="http://gramps-project.org/xml/1.7.2/"><header><created date="2026-01-01" version="6.0"/></header>
<people><person handle="_p1" change="1" id="I1"><gender>M</gender><name type="Birth Name"><first>Canary</first></name><childof hlink="_missing"/></person></people></database>`
	res := parseBytes(t, []byte(input))
	for _, w := range res.Warnings {
		if strings.Contains(w.Object+w.Target+w.Code+w.Kind, "Canary") {
			t.Fatalf("a warning holds a name: %+v", w)
		}
	}
}

func TestFlagsUseZeroAndOne(t *testing.T) {
	tree := &model.Tree{Header: model.Header{Created: model.Created{Date: "2026-01-01", Version: "6.0"}}}
	no := model.Flag(false)
	tree.People = []model.Person{{
		Object: model.Object{Handle: "_a", ID: "I1", Private: true, Change: 1},
		Gender: "M",
		Names:  []model.Name{{Alt: true, Surnames: []model.Surname{{Value: "X", Primary: &no}}}},
	}}
	data, err := Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`priv="1"`, `alt="1"`, `prim="0"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("output lacks %s:\n%s", want, text)
		}
	}
}
