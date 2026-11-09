// Package treesync keeps the tree of Heirfold equal to a Gramps database.
// The sync goes one way: Gramps is the source, and Heirfold never writes to it.
package treesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Renset/heirfold/internal/gramps"
	"github.com/Renset/heirfold/internal/grampsdb"
	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/store"
)

// metaKey is the setting that keeps the state between restarts.
const metaKey = "sync"

// Options configure a Syncer.
type Options struct {
	// Path is the Gramps tree directory or its sqlite.db file.
	Path string
	// Interval is the time between two checks of the file.
	Interval time.Duration
	// Limits protect the reader.
	Limits gramps.Limits
}

// Status describes the sync for the owner of the tree. It holds no names.
type Status struct {
	Source    string        `json:"source"`
	Name      string        `json:"name,omitempty"`
	CheckedAt int64         `json:"checkedAt,omitempty"`
	SyncedAt  int64         `json:"syncedAt,omitempty"`
	Counts    *model.Counts `json:"counts,omitempty"`
	Living    int           `json:"living"`
	Warnings  int           `json:"warnings"`
	Error     string        `json:"error,omitempty"`
}

// saved is the part of the state that survives a restart.
type saved struct {
	File        string         `json:"file"`
	Stamp       grampsdb.Stamp `json:"stamp"`
	Fingerprint string         `json:"fingerprint"`
	SyncedAt    int64          `json:"syncedAt"`
	Counts      model.Counts   `json:"counts"`
	Living      int            `json:"living"`
	Warnings    int            `json:"warnings"`
}

// Syncer reads the Gramps database when its file changes.
type Syncer struct {
	store    *store.Store
	file     string
	name     string
	interval time.Duration
	limits   gramps.Limits
	log      *slog.Logger
	now      func() time.Time

	run sync.Mutex // one sync at a time

	mu        sync.Mutex
	state     saved
	loaded    bool
	checkedAt int64
	lastError string
}

// New prepares the sync for one Gramps tree.
func New(st *store.Store, opts Options, log *slog.Logger) (*Syncer, error) {
	file, err := grampsdb.Locate(opts.Path)
	if err != nil {
		return nil, err
	}
	if opts.Interval <= 0 {
		opts.Interval = 3 * time.Second
	}
	if opts.Limits.MaxObjects <= 0 {
		opts.Limits = gramps.DefaultLimits
	}
	return &Syncer{
		store: st, file: file, name: grampsdb.TreeName(file), interval: opts.Interval,
		limits: opts.Limits, log: log, now: time.Now,
	}, nil
}

// File returns the database file that the syncer reads.
func (s *Syncer) File() string { return s.file }

// Fingerprint identifies the content of a tree. The export date of the header
// does not count.
func Fingerprint(t *model.Tree) (string, error) {
	meta := t.Meta()
	meta.Header.Created = model.Created{}
	head, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	records, err := t.Records()
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	sum.Write(head)
	for _, record := range records {
		sum.Write([]byte{0})
		sum.Write([]byte(record.Kind))
		sum.Write([]byte{0})
		sum.Write([]byte(record.Handle))
		sum.Write([]byte{0})
		sum.Write(record.Doc)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func (s *Syncer) load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	text, ok, err := s.store.MetaGet(ctx, metaKey)
	if err != nil {
		return err
	}
	if ok {
		var state saved
		if json.Unmarshal([]byte(text), &state) == nil && state.File == s.file {
			s.state = state
		}
	}
	s.loaded = true
	return nil
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, grampsdb.ErrBusy):
		return "busy"
	case errors.Is(err, grampsdb.ErrNotFound):
		return "not_found"
	case errors.Is(err, grampsdb.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, gramps.ErrTooManyObject):
		return "too_large"
	}
	return "failed"
}

// Sync reads the database if its file changed. With force it reads the
// database in any case. It reports whether the tree of Heirfold changed.
func (s *Syncer) Sync(ctx context.Context, force bool) (bool, error) {
	s.run.Lock()
	defer s.run.Unlock()
	changed, err := s.sync(ctx, force)
	s.mu.Lock()
	s.checkedAt = s.now().Unix()
	if err != nil {
		s.lastError = errorCode(err)
	} else {
		s.lastError = ""
	}
	s.mu.Unlock()
	return changed, err
}

func (s *Syncer) sync(ctx context.Context, force bool) (bool, error) {
	if err := s.load(ctx); err != nil {
		return false, err
	}
	stamp, err := grampsdb.Stat(s.file)
	if err != nil {
		return false, grampsdb.ErrNotFound
	}
	s.mu.Lock()
	state := s.state
	s.mu.Unlock()
	if !force && state.Fingerprint != "" && state.Stamp == stamp {
		return false, nil
	}
	result, err := grampsdb.Read(ctx, s.file, s.limits)
	if err != nil {
		return false, err
	}
	fingerprint, err := Fingerprint(result.Tree)
	if err != nil {
		return false, err
	}
	next := saved{
		File: s.file, Stamp: stamp, Fingerprint: fingerprint, SyncedAt: state.SyncedAt,
		Counts: result.Tree.Counts(), Living: state.Living, Warnings: result.WarningsTotal,
	}
	changed := false
	hasTree, err := s.store.HasTree(ctx)
	if err != nil {
		return false, err
	}
	if fingerprint != state.Fingerprint || !hasTree {
		living, err := s.store.ReplaceTree(ctx, result.Tree)
		if err != nil {
			return false, err
		}
		next.Living = living
		next.SyncedAt = s.now().Unix()
		changed = true
		// The log holds numbers only, never names.
		s.log.Info("tree synchronized from Gramps", "people", next.Counts.People, "families", next.Counts.Families,
			"media", next.Counts.Media, "living", living, "warnings", result.WarningsTotal)
	}
	text, err := json.Marshal(next)
	if err != nil {
		return changed, err
	}
	if err := s.store.MetaSet(ctx, metaKey, string(text)); err != nil {
		return changed, err
	}
	s.mu.Lock()
	s.state = next
	s.mu.Unlock()
	return changed, nil
}

// Run checks the file until the context ends.
func (s *Syncer) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	previous := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := s.Sync(ctx, false)
			code := ""
			if err != nil && ctx.Err() == nil {
				code = errorCode(err)
			}
			// Log a problem once, not on every check.
			if code != previous {
				if code != "" {
					s.log.Warn("sync from Gramps failed", "reason", code, "error", err)
				} else {
					s.log.Info("sync from Gramps works again")
				}
				previous = code
			}
		}
	}
}

// Status returns the state of the sync.
func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := Status{
		Source: "gramps", Name: s.name, CheckedAt: s.checkedAt, SyncedAt: s.state.SyncedAt,
		Living: s.state.Living, Warnings: s.state.Warnings, Error: s.lastError,
	}
	if s.state.Fingerprint != "" {
		counts := s.state.Counts
		status.Counts = &counts
	}
	return status
}
