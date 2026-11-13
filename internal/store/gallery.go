package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Renset/heirfold/internal/model"
)

// The photo gallery. A photo of the gallery is a media object of the tree. A
// mark on a face is a link from a person to the photo with the part of the
// picture that shows the face. Gramps stores a person in a photo in the same
// way, so the marks pass through a Gramps XML file without a change.

// GalleryFace is a person who is marked on a photo.
type GalleryFace struct {
	Person *Card `json:"person"`
	// Region is the part of the picture with the face, in percent. A link
	// without a part means that the person is somewhere in the picture.
	Region *model.Region `json:"region,omitempty"`
}

// PhotoPlace says where a photo was made. Lat and Lon are the point from the
// file in degrees, rounded to about ten metres; north and east are positive.
// The name is the name of the town or the village, never an address. A place
// far from every settlement has no name.
type PhotoPlace struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	// Name is the usual international name. NameRu is the Russian name.
	Name   string `json:"name,omitempty"`
	NameRu string `json:"nameRu,omitempty"`
	// Country is the code of the country in two letters.
	Country string `json:"country,omitempty"`
	// Manual is true when an editor chose the place. Such a place wins over
	// the place from the file.
	Manual bool `json:"manual,omitempty"`
}

// PhotoPlaceInput is the place of a photo that an editor chooses: a settlement of
// the list of settlements, or a place of the tree with a point on the globe.
type PhotoPlaceInput struct {
	// Remove takes the chosen place away. The place from the file shows again.
	Remove  bool    `json:"remove,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Name    string  `json:"name"`
	NameRu  string  `json:"nameRu"`
	Country string  `json:"country"`
}

// encodePlace writes a chosen place as the text of an attribute:
// "48.8566,2.3522|FR|Paris|Париж". A person who opens the tree in Gramps can
// read it, and the place needs no table outside the tree. The point is the
// point that the editor chose, rounded to about ten metres; the name is the
// name of the settlement.
func encodePlace(in *PhotoPlaceInput) (string, bool) {
	name, okName := cleanLine(in.Name, maxNameLength)
	nameRu, okRu := cleanLine(in.NameRu, maxNameLength)
	country := strings.ToUpper(strings.TrimSpace(in.Country))
	letters := func(text string) bool {
		for _, c := range text {
			if c < 'A' || c > 'Z' {
				return false
			}
		}
		return true
	}
	// A point far from every settlement has no name.
	if !okName || !okRu || (name == "" && nameRu != "") || strings.ContainsAny(name+nameRu, "|") ||
		(country != "" && (len(country) != 2 || !letters(country))) ||
		math.IsNaN(in.Lat) || math.IsNaN(in.Lon) || math.Abs(in.Lat) > 90 || math.Abs(in.Lon) > 180 {
		return "", false
	}
	return strconv.FormatFloat(math.Round(in.Lat*1e4)/1e4, 'f', -1, 64) + "," +
		strconv.FormatFloat(math.Round(in.Lon*1e4)/1e4, 'f', -1, 64) + "|" + country + "|" + name + "|" + nameRu, true
}

// parsePlace reads the text of encodePlace.
func parsePlace(text string) *PhotoPlace {
	parts := strings.Split(text, "|")
	if len(parts) != 4 {
		return nil
	}
	lat, lon, ok := strings.Cut(parts[0], ",")
	if !ok {
		return nil
	}
	place := &PhotoPlace{Country: parts[1], Name: parts[2], NameRu: parts[3], Manual: true}
	var errLat, errLon error
	place.Lat, errLat = strconv.ParseFloat(lat, 64)
	place.Lon, errLon = strconv.ParseFloat(lon, 64)
	if errLat != nil || errLon != nil || math.Abs(place.Lat) > 90 || math.Abs(place.Lon) > 180 {
		return nil
	}
	return place
}

// GalleryPhoto is one picture of the gallery.
type GalleryPhoto struct {
	ID          string          `json:"id"`
	Description string          `json:"description,omitempty"`
	Date        *model.DateView `json:"date,omitempty"`
	// Hidden is true while guests cannot see the photo.
	Hidden bool `json:"hidden,omitempty"`
	// Uploaded is true for a file that came through Heirfold. Only such a
	// photo can leave the gallery.
	Uploaded bool `json:"uploaded,omitempty"`
	// Taken is the time of the picture on the clock of the camera, as
	// "2006-01-02T15:04:05". The server reads it from the file.
	Taken string `json:"taken,omitempty"`
	// Year is the year of the picture for the filter: the year of the date
	// that an editor wrote, or else the year from the file. 0 means no year.
	Year int `json:"year,omitempty"`
	// Place is where the camera was, if the file says a place.
	Place *PhotoPlace `json:"place,omitempty"`
	// AddedBy and AddedAt say who brought the file through Heirfold, and when.
	// Dated is the date of the photo in the form of the tree, for the edit form.
	Dated   *model.Dated  `json:"dated,omitempty"`
	AddedBy string        `json:"addedBy,omitempty"`
	AddedAt int64         `json:"addedAt,omitempty"`
	Faces   []GalleryFace `json:"faces"`
	// change orders the photos: the newest first.
	change int64
}

// Gallery returns the photos that the scope can see, the newest first, with
// the people who are marked on them. The scan of a document is no photo and
// stays out: a picture that a source or a citation shows, unless somebody
// added it in the gallery. A reader finds such a scan at its source.
func (v *View) Gallery(ctx context.Context) ([]GalleryPhoto, error) {
	// The marks live in the people, so one pass over the people finds them.
	marks, err := v.faceMarks(ctx)
	if err != nil {
		return nil, err
	}

	evidence, err := v.evidenceMedia(ctx)
	if err != nil {
		return nil, err
	}
	fromFile, err := v.mediaInfo(ctx)
	if err != nil {
		return nil, err
	}
	added, err := v.mediaAdded(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := v.db.QueryContext(ctx, `SELECT m.handle, m.gid, m.private, m.living, m.path, m.mime, m.description, m.date, m.gallery, o.change,
			json_object('dateval', json(json_extract(o.doc, '$.dateval')), 'daterange', json(json_extract(o.doc, '$.daterange')),
				'datespan', json(json_extract(o.doc, '$.datespan')), 'datestr', json(json_extract(o.doc, '$.datestr'))),
			(SELECT json_extract(a.value, '$.value') FROM json_each(o.doc, '$.attributes') a
				WHERE json_extract(a.value, '$.type') = '`+attrPlace+`' LIMIT 1)
		FROM media_index m JOIN object o ON o.kind = 'media' AND o.handle = m.handle`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	type entry struct {
		handle string
		photo  GalleryPhoto
	}
	var entries []entry
	for rows.Next() {
		var handle string
		var row mediaRow
		var change int64
		var gallery bool
		var dated string
		var placeText sql.NullString
		if err := rows.Scan(&handle, &row.gid, &row.private, &row.living, &row.path, &row.mime, &row.description, &row.date, &gallery, &change, &dated, &placeText); err != nil {
			return nil, err
		}
		if !IsImage(row.mime) || !v.allows(row.private, row.living) || (evidence[handle] && !gallery) {
			continue
		}
		view := row.view()
		photo := GalleryPhoto{
			ID: view.ID, Description: view.Description, Date: view.Date, Hidden: row.private,
			Uploaded: strings.HasPrefix(row.path, uploadDir+"/"), Faces: []GalleryFace{}, change: change,
		}
		if view.Date != nil {
			var raw model.Dated
			if json.Unmarshal([]byte(dated), &raw) == nil && raw.HasDate() {
				photo.Dated = &raw
			}
		}
		info := fromFile[handle]
		photo.Taken = info.Taken
		if chosen := parsePlace(placeText.String); chosen != nil {
			photo.Place = chosen
		} else if info.HasPlace {
			photo.Place = &PhotoPlace{
				Lat: math.Round(info.Lat*1e4) / 1e4, Lon: math.Round(info.Lon*1e4) / 1e4,
				Name: info.Town, NameRu: info.TownRu, Country: info.Country,
			}
		}
		// The date that a person wrote is about the picture. The time in the
		// file of an old photo is the day of the scan.
		switch {
		case view.Date != nil && view.Date.Start != nil && view.Date.Start.Year != 0:
			photo.Year = view.Date.Start.Year
		case view.Date == nil && len(info.Taken) >= 4:
			photo.Year, _ = strconv.Atoi(info.Taken[:4])
		}
		if who, ok := added[handle]; ok {
			photo.AddedBy, photo.AddedAt = who.name, who.at
		}
		entries = append(entries, entry{handle: handle, photo: photo})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]GalleryPhoto, 0, len(entries))
	for _, item := range entries {
		for _, found := range marks[item.handle] {
			card, err := v.card(ctx, found.person)
			if err != nil {
				return nil, err
			}
			if card != nil {
				item.photo.Faces = append(item.photo.Faces, GalleryFace{Person: card, Region: found.region})
			}
		}
		out = append(out, item.photo)
	}
	slices.SortStableFunc(out, func(a, b GalleryPhoto) int {
		return cmp.Or(cmp.Compare(b.change, a.change), cmp.Compare(b.ID, a.ID))
	})
	return out, nil
}

// evidenceMedia returns the handles of the files that a source or a citation
// shows. Such a picture is the scan of a document: a page of a parish book, a
// letter, a certificate. A picture that only people, families, events or
// places show is a photo.
func (v *View) evidenceMedia(ctx context.Context) (map[string]bool, error) {
	rows, err := v.db.QueryContext(ctx, `SELECT DISTINCT json_extract(ref.value, '$.h')
		FROM object o, json_each(o.doc, '$.mediaRefs') ref WHERE o.kind IN ('citation', 'source')`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var handle sql.NullString
		if err := rows.Scan(&handle); err != nil {
			return nil, err
		}
		if handle.Valid {
			out[handle.String] = true
		}
	}
	return out, rows.Err()
}

// galleryMedia returns a picture by its ID.
func (e *Edit) galleryMedia(gid string) (*model.Media, error) {
	handle, ok := e.handleOf(model.KindMedia, gid)
	if !ok {
		return nil, ErrNotFound
	}
	item := e.media[handle]
	if item == nil || !IsImage(item.File.Mime) {
		return nil, ErrNotFound
	}
	return item, nil
}

// AddGalleryPhoto adds an uploaded picture to the gallery. The picture has
// no person yet. It starts hidden from guests: the server cannot know who is
// in a photo, and a mistake must hide a dead person, never show a living one.
func (e *Edit) AddGalleryPhoto(src, mime, checksum string) error {
	if !strings.HasPrefix(src, uploadDir+"/") || strings.Contains(src, "..") {
		return ErrInvalid
	}
	for handle, item := range e.media {
		if item.File.Src == src && !e.deleted[recordKey(model.KindMedia, handle)] {
			// The same file is one photo. A picture of one person that
			// comes again through the gallery becomes a photo of the gallery.
			if setInGallery(item) {
				e.touch(model.KindMedia, handle)
			}
			e.Created = item.ID
			return nil
		}
	}
	item := &model.Media{File: model.MediaFile{Src: src, Mime: mime, Checksum: checksum}}
	item.Private = true
	setInGallery(item)
	e.register(model.KindMedia, &item.Object)
	e.media[item.Handle] = item
	e.Created = item.ID
	return nil
}

// GalleryInput holds what an editor changes about a photo.
type GalleryInput struct {
	Description string `json:"description"`
	// Date is when the picture was made, as far as the family knows. Without
	// it the photo keeps the date that it has.
	Date *model.Dated `json:"date,omitempty"`
	// Place is the place that the editor chose. Without it the photo keeps
	// the place that it has.
	Place *PhotoPlaceInput `json:"place,omitempty"`
	// Hidden keeps the photo away from guests.
	Hidden bool `json:"hidden"`
}

// UpdateGalleryPhoto changes the title of a photo and who can see it.
func (e *Edit) UpdateGalleryPhoto(gid string, in GalleryInput) error {
	item, err := e.galleryMedia(gid)
	if err != nil {
		return err
	}
	description, ok := cleanLine(in.Description, maxNameLength)
	if !ok {
		return ErrInvalid
	}
	if in.Date != nil {
		if !validDate(in.Date) {
			return ErrInvalid
		}
		item.Dated = *in.Date
	}
	if in.Place != nil {
		item.Attributes = slices.DeleteFunc(item.Attributes, func(a model.Attribute) bool { return a.Type == attrPlace })
		if !in.Place.Remove {
			text, ok := encodePlace(in.Place)
			if !ok {
				return ErrInvalid
			}
			item.Attributes = append(item.Attributes, model.Attribute{Type: attrPlace, Value: text})
		}
		if len(item.Attributes) == 0 {
			item.Attributes = nil
		}
	}
	item.File.Description = description
	item.Private = model.Flag(in.Hidden)
	e.touch(model.KindMedia, item.Handle)
	return nil
}

// MarkFace says where a person is on a photo. A second mark of the same
// person moves the first one. A mark is no portrait: the person, or an
// editor, chooses the portrait among the pictures of the person.
func (e *Edit) MarkFace(gid, personID string, region model.Region) error {
	item, err := e.galleryMedia(gid)
	if err != nil {
		return err
	}
	person, err := e.Person(personID)
	if err != nil {
		return err
	}
	if !validRegion(&region) {
		return ErrInvalid
	}
	at := slices.IndexFunc(person.MediaRefs, func(ref model.MediaRef) bool { return ref.HLink == item.Handle })
	if at >= 0 {
		person.MediaRefs[at].Region = &region
	} else {
		ref := model.MediaRef{HLink: item.Handle, Region: &region}
		setNotPortrait(&ref, true)
		person.MediaRefs = append(person.MediaRefs, ref)
	}
	e.touch(model.KindPerson, person.Handle)
	e.Subject = personID
	return nil
}

// UnmarkFace takes a person off a photo. The photo stays in the gallery.
func (e *Edit) UnmarkFace(gid, personID string) error {
	item, err := e.galleryMedia(gid)
	if err != nil {
		return err
	}
	person, err := e.Person(personID)
	if err != nil {
		return err
	}
	if !e.dropPicture(person, item.Handle) {
		return ErrNotFound
	}
	e.touch(model.KindPerson, person.Handle)
	e.Subject = personID
	return nil
}

// RemoveGalleryPhoto takes an uploaded photo out of the tree, with the marks
// on it. The file stays on the disk, so an undo brings the photo back. A
// file that came from Gramps stays: other records can use it as a source.
func (e *Edit) RemoveGalleryPhoto(gid string) error {
	item, err := e.galleryMedia(gid)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(item.File.Src, uploadDir+"/") {
		return ErrInvalid
	}
	// A source or a citation that shows the photo keeps it. The edit page has no way to change those links.
	used := func(refs []model.MediaRef) bool {
		return slices.ContainsFunc(refs, func(ref model.MediaRef) bool { return ref.HLink == item.Handle })
	}
	for i := range e.tree.Citations {
		if used(e.tree.Citations[i].MediaRefs) {
			return ErrInvalid
		}
	}
	for i := range e.tree.Sources {
		if used(e.tree.Sources[i].MediaRefs) {
			return ErrInvalid
		}
	}
	drop := func(refs []model.MediaRef) ([]model.MediaRef, bool) {
		out := slices.DeleteFunc(refs, func(ref model.MediaRef) bool { return ref.HLink == item.Handle })
		return out, len(out) != len(refs)
	}
	for handle, person := range e.people {
		if e.dropPicture(person, item.Handle) {
			e.touch(model.KindPerson, handle)
		}
	}
	for handle, family := range e.families {
		if refs, changed := drop(family.MediaRefs); changed {
			family.MediaRefs = refs
			e.touch(model.KindFamily, handle)
		}
	}
	for handle, event := range e.events {
		if refs, changed := drop(event.MediaRefs); changed {
			event.MediaRefs = refs
			e.touch(model.KindEvent, handle)
		}
	}
	for handle, place := range e.places {
		if refs, changed := drop(place.MediaRefs); changed {
			place.MediaRefs = refs
			e.touch(model.KindPlace, handle)
		}
	}
	e.remove(model.KindMedia, item.Handle)
	return nil
}

// TreePlace is a place of the tree that has a point on the globe.
type TreePlace struct {
	Name     string
	Lat, Lon float64
}

// PlacesWithPoints returns the places of the tree that have a point on the
// globe as two plain numbers, sorted by name. A family tree names villages
// that no list of settlements has.
func (e *Edit) PlacesWithPoints() []TreePlace {
	number := func(text string) (float64, bool) {
		value, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(text), ",", "."), 64)
		return value, err == nil && !math.IsNaN(value)
	}
	var out []TreePlace
	for handle, place := range e.places {
		if e.deleted[recordKey(model.KindPlace, handle)] || place.Coord == nil {
			continue
		}
		name := strings.TrimSpace(place.Title)
		if name == "" && len(place.Names) > 0 {
			name = strings.TrimSpace(place.Names[0].Value)
		}
		lat, okLat := number(place.Coord.Lat)
		lon, okLon := number(place.Coord.Long)
		if name == "" || !okLat || !okLon || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
			continue
		}
		out = append(out, TreePlace{Name: name, Lat: lat, Lon: lon})
	}
	slices.SortFunc(out, func(a, b TreePlace) int { return strings.Compare(a.Name, b.Name) })
	return out
}
