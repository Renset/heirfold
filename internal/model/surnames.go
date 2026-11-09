package model

import "strings"

// The surname that the app shows for a person.
//
// Gramps keeps every name of a person, each with a type: "Birth Name",
// "Married Name", "Also Known As". Gramps does not say which name to show; a
// tree often shows women under the maiden name in one place and under the
// married name in another. Heirfold shows the surname that the person carried
// last, because relatives and documents of the later life know the person by
// it, and adds the surname at birth when it differs: «Иванова (урожд.
// Сидорова)». The rule reads the types of the names, so it gives the same
// answer whichever name is the primary one.

// Name types of Gramps.
const (
	NameBirth   = "Birth Name"
	NameMarried = "Married Name"
	NameAKA     = "Also Known As"
)

// surnameOf returns the main surname of a name, and whether it is a surname
// that the person took in marriage: the derivation "Taken" of Gramps.
func surnameOf(n Name) (string, bool) {
	surname, _ := SplitName(n)
	if surname == "" {
		return "", false
	}
	for _, s := range n.Surnames {
		value := strings.TrimSpace(s.Value)
		if s.Prefix != "" {
			value = s.Prefix + " " + value
		}
		if value == surname {
			return surname, s.Derivation == "Taken"
		}
	}
	return surname, false
}

// isMarriedName reports whether a name is a name from a marriage. The type of
// the name decides: a person chooses it in a form and sees it. The origin of
// the surname is a hidden detail and is often wrong, so it counts only for a
// name without a type: a surname taken in marriage.
func isMarriedName(n Name) bool {
	switch kind := strings.TrimSpace(n.Type); {
	case strings.EqualFold(kind, NameMarried):
		return true
	case kind == "":
		_, taken := surnameOf(n)
		return taken
	}
	return false
}

// isBirthName reports whether a name can hold the surname at birth: a name of
// the type "Birth Name", whatever the origin of its surname, or a name without
// a type whose surname was not taken in marriage.
func isBirthName(n Name) bool {
	kind := strings.TrimSpace(n.Type)
	return strings.EqualFold(kind, NameBirth) || (kind == "" && !isMarriedName(n))
}

// Surnames returns the surname to show for a person and the surname at birth.
// A name from a marriage is a name of the type "Married Name", or a name
// without a type whose surname has the origin "Taken".
// names are the names that the viewer can see, the primary name first or
// marked by Alt. birth is empty when it is the same as shown, or unknown.
// married is true when shown comes from a name from a marriage.
//
// The surname to show is the surname of the latest name from a marriage: the
// one with the latest date, or else the primary name if it is from a marriage,
// or else the last such name in the list. Without such a name it is the
// surname of the primary name, and without that the surname at birth.
func Surnames(names []Name) (shown, birth string, married bool) {
	if len(names) == 0 {
		return "", "", false
	}
	primary := 0
	for i, n := range names {
		if !n.Alt {
			primary = i
			break
		}
	}
	// The surname at birth: the primary name if it can hold it, else the first such name.
	birthAt := -1
	if isBirthName(names[primary]) {
		birthAt = primary
	} else {
		for i, n := range names {
			if isBirthName(n) {
				if s, _ := surnameOf(n); s != "" {
					birthAt = i
					break
				}
			}
		}
	}
	if birthAt >= 0 {
		birth, _ = surnameOf(names[birthAt])
	}

	marriedAt := -1
	var latest int64
	for i, n := range names {
		if !isMarriedName(n) {
			continue
		}
		if s, _ := surnameOf(n); s == "" {
			continue
		}
		key := int64(0)
		if view := n.View(); view != nil {
			key = view.SortKey()
		}
		switch {
		case marriedAt < 0:
			marriedAt, latest = i, key
		case key != 0 && key >= latest:
			marriedAt, latest = i, key
		case latest == 0 && key == 0 && marriedAt != primary:
			// Without dates the primary name wins, else the last one in the list.
			marriedAt = i
		}
	}

	if marriedAt >= 0 {
		shown, _ = surnameOf(names[marriedAt])
		married = true
	} else if shown, _ = surnameOf(names[primary]); shown == "" {
		shown = birth
	}
	if strings.EqualFold(strings.TrimSpace(birth), strings.TrimSpace(shown)) {
		birth = ""
	}
	return shown, birth, married
}
