// Package snapshot writes a tree as one static JSON file for read-only hosting.
package snapshot

import (
	"context"

	"github.com/Renset/heirfold/internal/store"
)

// FormatVersion is the version of the bundle format.
const FormatVersion = 1

// Bundle holds everything that a static frontend shows.
type Bundle struct {
	Format int                            `json:"format"`
	Role   string                         `json:"role"`
	Graph  *store.Graph                   `json:"graph"`
	People map[string]*store.PersonDetail `json:"people"`
}

// Build creates a bundle through a View, so the same privacy filter applies as
// on the server.
func Build(ctx context.Context, view *store.View, role string) (*Bundle, error) {
	graph, err := view.Graph(ctx)
	if err != nil {
		return nil, err
	}
	bundle := &Bundle{Format: FormatVersion, Role: role, Graph: graph, People: make(map[string]*store.PersonDetail, len(graph.People))}
	for _, card := range graph.People {
		detail, err := view.Person(ctx, card.ID)
		if err != nil {
			return nil, err
		}
		bundle.People[card.ID] = detail
	}
	return bundle, nil
}
