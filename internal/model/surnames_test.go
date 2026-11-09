package model

import "testing"

func name(kind, surname string, alt bool) Name {
	n := Name{Type: kind, First: "Anna", Alt: Flag(alt)}
	if surname != "" {
		n.Surnames = []Surname{{Value: surname}}
	}
	return n
}

func TestSurnames(t *testing.T) {
	taken := name("", "Ivanova", false)
	taken.Surnames[0].Derivation = "Taken"
	// A tree from Gramps can have the origin "Taken" on a surname at birth.
	// The type of the name decides.
	takenAtBirth := name("Birth Name", "Ivanova", true)
	takenAtBirth.Surnames[0].Derivation = "Taken"
	marriedTaken := name("Married Name", "Petrova", false)
	marriedTaken.Surnames[0].Derivation = "Taken"
	dated := func(n Name, year string) Name {
		n.DateVal = &DateVal{Val: year}
		return n
	}
	withPatronymic := name("Married Name", "Ivanova", false)
	withPatronymic.Surnames = append([]Surname{{Value: "Petrovna", Derivation: "Patronymic"}}, withPatronymic.Surnames...)

	for _, c := range []struct {
		about        string
		names        []Name
		shown, birth string
		married      bool
	}{
		{"no names", nil, "", "", false},
		{"one name at birth", []Name{name("Birth Name", "Sidorova", false)}, "Sidorova", "", false},
		{"a name without a type", []Name{name("", "Sidorova", false)}, "Sidorova", "", false},
		{"the maiden name is primary, the married name is another",
			[]Name{name("Birth Name", "Sidorova", false), name("Married Name", "Ivanova", true)}, "Ivanova", "Sidorova", true},
		{"the married name is primary, the maiden name is another",
			[]Name{name("Married Name", "Ivanova", false), name("Birth Name", "Sidorova", true)}, "Ivanova", "Sidorova", true},
		{"only the married name is known", []Name{name("Married Name", "Ivanova", false)}, "Ivanova", "", true},
		{"a surname taken in marriage, in a name without a type", []Name{taken, name("Birth Name", "Sidorova", true)}, "Ivanova", "Sidorova", true},
		{"a name at birth with the origin taken in marriage", []Name{marriedTaken, takenAtBirth}, "Petrova", "Ivanova", true},
		{"only a name at birth with the origin taken in marriage", []Name{takenAtBirth}, "Ivanova", "", false},
		{"the same surname in both names", []Name{name("Birth Name", "Ivanova", false), name("Married Name", "ivanova", true)}, "ivanova", "", true},
		{"another name is no surname at birth",
			[]Name{name("Married Name", "Ivanova", false), name("Also Known As", "Sidorova", true)}, "Ivanova", "", true},
		{"two marriages without dates: the last one",
			[]Name{name("Birth Name", "Sidorova", false), name("Married Name", "Ivanova", true), name("Married Name", "Petrova", true)}, "Petrova", "Sidorova", true},
		{"two marriages without dates: the primary one",
			[]Name{name("Birth Name", "Sidorova", true), name("Married Name", "Ivanova", false), name("Married Name", "Petrova", true)}, "Ivanova", "Sidorova", true},
		{"two marriages with dates: the latest",
			[]Name{name("Birth Name", "Sidorova", false), dated(name("Married Name", "Petrova", true), "1960"), dated(name("Married Name", "Ivanova", true), "1950")}, "Petrova", "Sidorova", true},
		{"a married name without a surname", []Name{name("Birth Name", "Sidorova", false), name("Married Name", "", true)}, "Sidorova", "", false},
		{"no surname at all", []Name{name("Birth Name", "", false)}, "", "", false},
		{"another name chosen as the primary one", []Name{name("Also Known As", "Orloff", false), name("Birth Name", "Orlov", true)}, "Orloff", "Orlov", false},
		{"a primary name without a surname", []Name{name("Also Known As", "", false), name("Birth Name", "Orlov", true)}, "Orlov", "", false},
		{"a patronymic is no surname", []Name{withPatronymic, name("Birth Name", "Sidorova", true)}, "Ivanova", "Sidorova", true},
	} {
		shown, birth, married := Surnames(c.names)
		if shown != c.shown || birth != c.birth || married != c.married {
			t.Errorf("%s: got %q, %q, %v; want %q, %q, %v", c.about, shown, birth, married, c.shown, c.birth, c.married)
		}
	}
}
