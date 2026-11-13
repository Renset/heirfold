package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Renset/heirfold/internal/model"
)

// Photo points to the image that stands for a person.
type Photo struct {
	ID     string        `json:"id"`
	Region *model.Region `json:"region,omitempty"`
}

// MediaView describes a media object that the viewer can see.
type MediaView struct {
	ID          string          `json:"id"`
	Description string          `json:"description,omitempty"`
	Mime        string          `json:"mime,omitempty"`
	Image       bool            `json:"image,omitempty"`
	Date        *model.DateView `json:"date,omitempty"`
	Region      *model.Region   `json:"region,omitempty"`
}

// MediaFile tells the server where the file of a media object is.
type MediaFile struct {
	ID          string
	Path        string
	Mime        string
	Checksum    string
	Description string
}

// IsImage reports whether Heirfold can show a file of this type as a picture.
func IsImage(mime string) bool {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/jpg", "image/png", "image/gif":
		return true
	}
	return false
}

// mediaOfLiving returns the handles of the media that a living person leads
// to: the media of that person, of the events, the families and the citations
// of that person, and of the sources behind those citations. A guest must not
// see such a file, because it can show the person.
func mediaOfLiving(t *model.Tree, living map[string]bool) map[string]bool {
	events := make(map[string]*model.Event, len(t.Events))
	for i := range t.Events {
		events[t.Events[i].Handle] = &t.Events[i]
	}
	families := make(map[string]*model.Family, len(t.Families))
	for i := range t.Families {
		families[t.Families[i].Handle] = &t.Families[i]
	}
	citations := make(map[string]*model.Citation, len(t.Citations))
	for i := range t.Citations {
		citations[t.Citations[i].Handle] = &t.Citations[i]
	}
	sources := make(map[string]*model.Source, len(t.Sources))
	for i := range t.Sources {
		sources[t.Sources[i].Handle] = &t.Sources[i]
	}

	out := map[string]bool{}
	seenCitation := map[string]bool{}
	var addMedia func(refs []model.MediaRef)
	addCitations := func(refs []model.Ref) {
		for _, ref := range refs {
			if seenCitation[ref.HLink] {
				continue
			}
			seenCitation[ref.HLink] = true
			citation := citations[ref.HLink]
			if citation == nil {
				continue
			}
			addMedia(citation.MediaRefs)
			if source := sources[citation.Source.HLink]; source != nil {
				addMedia(source.MediaRefs)
			}
		}
	}
	addMedia = func(refs []model.MediaRef) {
		for _, ref := range refs {
			out[ref.HLink] = true
			addCitations(ref.Citations)
		}
	}
	addAttributes := func(list []model.Attribute) {
		for _, attribute := range list {
			addCitations(attribute.Citations)
		}
	}
	addEvents := func(refs []model.EventRef) {
		for _, ref := range refs {
			addCitations(ref.Citations)
			addAttributes(ref.Attributes)
			if event := events[ref.HLink]; event != nil {
				addMedia(event.MediaRefs)
				addCitations(event.Citations)
				addAttributes(event.Attributes)
			}
		}
	}
	addFamily := func(handle string) {
		family := families[handle]
		if family == nil {
			return
		}
		addMedia(family.MediaRefs)
		addCitations(family.Citations)
		addAttributes(family.Attributes)
		addEvents(family.EventRefs)
		for _, child := range family.ChildRefs {
			addCitations(child.Citations)
		}
		for _, ord := range family.LDS {
			addCitations(ord.Citations)
		}
	}
	// A file that nothing points to has no known subject. An edit can leave
	// the portrait of a living person in that state, so a guest gets none.
	referenced := map[string]bool{}
	mark := func(refs []model.MediaRef) {
		for _, ref := range refs {
			referenced[ref.HLink] = true
		}
	}
	for i := range t.People {
		mark(t.People[i].MediaRefs)
	}
	for i := range t.Families {
		mark(t.Families[i].MediaRefs)
	}
	for i := range t.Events {
		mark(t.Events[i].MediaRefs)
	}
	for i := range t.Citations {
		mark(t.Citations[i].MediaRefs)
	}
	for i := range t.Sources {
		mark(t.Sources[i].MediaRefs)
	}
	for i := range t.Places {
		mark(t.Places[i].MediaRefs)
	}
	for i := range t.Media {
		if !referenced[t.Media[i].Handle] {
			out[t.Media[i].Handle] = true
		}
	}
	for i := range t.People {
		person := &t.People[i]
		if !living[person.Handle] {
			continue
		}
		addMedia(person.MediaRefs)
		addCitations(person.Citations)
		addAttributes(person.Attributes)
		addEvents(person.EventRefs)
		for _, name := range person.Names {
			addCitations(name.Citations)
		}
		for _, address := range person.Addresses {
			addCitations(address.Citations)
		}
		for _, ord := range person.LDS {
			addCitations(ord.Citations)
		}
		for _, ref := range person.PersonRefs {
			addCitations(ref.Citations)
		}
		for _, ref := range person.ChildOf {
			addFamily(ref.HLink)
		}
		for _, ref := range person.ParentIn {
			addFamily(ref.HLink)
		}
	}
	return out
}

// personPhoto returns the portrait of a person: the first picture that no
// mark keeps from that role. For the card that a guest sees, restricted names
// the files that a living person leads to: the card skips them, and it skips
// private pictures. For the full card it is nil.
func personPhoto(person *model.Person, media map[string]*model.Media, ids map[string]string, restricted map[string]bool) *Photo {
	for _, ref := range person.MediaRefs {
		item := media[ref.HLink]
		if item == nil || !IsImage(item.File.Mime) || notPortrait(&ref) {
			continue
		}
		if restricted != nil && (bool(ref.Private) || bool(item.Private) || restricted[item.Handle]) {
			continue
		}
		return &Photo{ID: ids[recordKey(model.KindMedia, item.Handle)], Region: ref.Region}
	}
	return nil
}

// writeMediaIndex replaces the index of media objects.
func writeMediaIndex(ctx context.Context, tx *sql.Tx, t *model.Tree, ids map[string]string, living map[string]bool) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM media_index`); err != nil {
		return err
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO media_index (handle, gid, private, living, path, mime, checksum, description, date, gallery) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = insert.Close() }()
	restricted := mediaOfLiving(t, living)
	for i := range t.Media {
		item := &t.Media[i]
		date := ""
		if view := item.View(); view != nil {
			data, err := json.Marshal(view)
			if err != nil {
				return err
			}
			date = string(data)
		}
		gid := ids[recordKey(model.KindMedia, item.Handle)]
		if _, err := insert.ExecContext(ctx, item.Handle, gid, bool(item.Private), restricted[item.Handle],
			item.File.Src, item.File.Mime, item.File.Checksum, item.File.Description, date, inGallery(item)); err != nil {
			return err
		}
	}
	return nil
}

type mediaRow struct {
	gid, path, mime, checksum, description, date string
	private, living                              bool
}

func (m *mediaRow) view() MediaView {
	view := MediaView{ID: m.gid, Description: m.description, Mime: m.mime, Image: IsImage(m.mime)}
	if m.date != "" {
		var date model.DateView
		if json.Unmarshal([]byte(m.date), &date) == nil {
			view.Date = &date
		}
	}
	return view
}

const mediaColumns = `gid, private, living, path, mime, checksum, description, date`

func scanMedia(row interface{ Scan(...any) error }) (*mediaRow, error) {
	var m mediaRow
	if err := row.Scan(&m.gid, &m.private, &m.living, &m.path, &m.mime, &m.checksum, &m.description, &m.date); err != nil {
		return nil, err
	}
	return &m, nil
}

// media returns the media objects behind the links that the scope can see.
func (v *View) media(ctx context.Context, refs []model.MediaRef) ([]MediaView, error) {
	var out []MediaView
	for _, ref := range refs {
		if !v.sees(ref.Private) {
			continue
		}
		row, err := scanMedia(v.db.QueryRowContext(ctx, `SELECT `+mediaColumns+` FROM media_index WHERE handle = ?`, ref.HLink))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !v.allows(row.private, row.living) {
			continue
		}
		view := row.view()
		view.Region = ref.Region
		out = append(out, view)
	}
	return out, nil
}

// MediaFile returns the file of one media object, or ErrNotFound if the
// object does not exist or the scope hides it.
func (v *View) MediaFile(ctx context.Context, gid string) (*MediaFile, error) {
	row, err := scanMedia(v.db.QueryRowContext(ctx, `SELECT `+mediaColumns+` FROM media_index WHERE gid = ?`, gid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !v.allows(row.private, row.living) {
		return nil, ErrNotFound
	}
	return &MediaFile{ID: row.gid, Path: row.path, Mime: row.mime, Checksum: row.checksum, Description: row.description}, nil
}

// MediaList returns all media objects that the scope can see.
func (v *View) MediaList(ctx context.Context) ([]MediaView, error) {
	rows, err := v.db.QueryContext(ctx, `SELECT `+mediaColumns+` FROM media_index ORDER BY gid`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []MediaView{}
	for rows.Next() {
		row, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		if v.allows(row.private, row.living) {
			out = append(out, row.view())
		}
	}
	return out, rows.Err()
}
