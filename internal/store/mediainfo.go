package store

import (
	"context"
	"database/sql"
)

// What a camera wrote into a picture: the time and the place.
// The server reads it from the file, so it is no part of the tree: an export
// does not carry it, the journal does not record it, and the server can read
// it again at any time.

// MediaInfo holds what the file of a picture says about the picture.
type MediaInfo struct {
	// Taken is the time on the clock of the camera, as "2006-01-02T15:04:05", or "".
	Taken string
	// HasPlace is true when the file names the place of the picture.
	HasPlace bool
	Lat, Lon float64
	// Town is the settlement of the place, if the list of settlements has
	// one near the place.
	Town, TownRu, Country string
}

// InfoJob is a picture whose file the server has not read yet.
type InfoJob struct {
	Handle, ID, Path, Checksum string
}

// InfoJobs returns the pictures without a record, or whose file changed. It
// first forgets the records of pictures that are gone.
func (s *Store) InfoJobs(ctx context.Context, limit int) ([]InfoJob, error) {
	if _, err := s.w.ExecContext(ctx, `DELETE FROM media_info WHERE media NOT IN (SELECT handle FROM media_index)`); err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `SELECT m.handle, m.gid, m.path, m.checksum, m.mime FROM media_index m
		LEFT JOIN media_info i ON i.media = m.handle
		WHERE i.media IS NULL OR i.path <> m.path OR i.checksum <> m.checksum
		ORDER BY m.gid`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []InfoJob
	for rows.Next() && len(out) < limit {
		var job InfoJob
		var mime string
		if err := rows.Scan(&job.Handle, &job.ID, &job.Path, &job.Checksum, &mime); err != nil {
			return nil, err
		}
		if IsImage(mime) {
			out = append(out, job)
		}
	}
	return out, rows.Err()
}

// SaveInfo stores what the file of a picture says. A file that the server
// cannot read gets an empty record, so the server does not try it each time.
func (s *Store) SaveInfo(ctx context.Context, job InfoJob, info MediaInfo) error {
	var lat, lon any
	if info.HasPlace {
		lat, lon = info.Lat, info.Lon
	}
	_, err := s.w.ExecContext(ctx, `INSERT INTO media_info (media, path, checksum, taken, lat, lon, town, town_ru, country, at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (media) DO UPDATE SET path = excluded.path, checksum = excluded.checksum, taken = excluded.taken,
		  lat = excluded.lat, lon = excluded.lon, town = excluded.town, town_ru = excluded.town_ru,
		  country = excluded.country, at = excluded.at`,
		job.Handle, job.Path, job.Checksum, info.Taken, lat, lon, info.Town, info.TownRu, info.Country, s.now().Unix())
	return err
}

// mediaInfo returns the records of all pictures by the handle of the picture.
func (v *View) mediaInfo(ctx context.Context) (map[string]MediaInfo, error) {
	rows, err := v.db.QueryContext(ctx, `SELECT media, taken, lat, lon, town, town_ru, country FROM media_info`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]MediaInfo{}
	for rows.Next() {
		var handle string
		var info MediaInfo
		var lat, lon sql.NullFloat64
		if err := rows.Scan(&handle, &info.Taken, &lat, &lon, &info.Town, &info.TownRu, &info.Country); err != nil {
			return nil, err
		}
		if lat.Valid && lon.Valid {
			info.HasPlace, info.Lat, info.Lon = true, lat.Float64, lon.Float64
		}
		out[handle] = info
	}
	return out, rows.Err()
}

// addedBy is who brought a file into the tree, and when.
type addedBy struct {
	name string
	at   int64
}

// mediaAdded returns, for each file that came through Heirfold, the first
// entry of the journal that made the file. A file from Gramps has no entry.
func (v *View) mediaAdded(ctx context.Context) (map[string]addedBy, error) {
	rows, err := v.db.QueryContext(ctx, `SELECT i.handle, s.actor, s.at FROM change_item i JOIN change_set s ON s.id = i.set_id
		WHERE i.kind = 'media' AND i.before IS NULL AND s.undoes IS NULL ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]addedBy{}
	for rows.Next() {
		var handle string
		var entry addedBy
		if err := rows.Scan(&handle, &entry.name, &entry.at); err != nil {
			return nil, err
		}
		if _, known := out[handle]; !known {
			out[handle] = entry
		}
	}
	return out, rows.Err()
}
