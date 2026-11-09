package privacy

// Scope says which records a viewer can see.
type Scope struct {
	// Private allows records that have the private flag.
	Private bool
	// Living allows people that Heirfold treats as living.
	Living bool
	// Notes allows the hidden notes: the notes with the private flag. Such a
	// note holds what the keepers of the tree do not tell every relative, so
	// only the people who can change the whole tree read it.
	Notes bool
}

// Public is the narrowest scope: no private records and no living people.
var Public = Scope{}

// Family is the scope of a family member: the whole tree with its private
// records and its living people, without the hidden notes.
var Family = Scope{Private: true, Living: true}

// Full is the scope with no limits.
var Full = Scope{Private: true, Living: true, Notes: true}
