package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"

	"github.com/Renset/heirfold/internal/model"
)

// The faces that the server finds on the pictures of the gallery. They are
// no part of the tree. A face becomes a part of the tree only when an editor
// marks a person on it: then the mark is a link from the person to the
// picture, as for a mark by hand.

// FoundFace is a face that the server found on a picture. The places are
// percent of the stored picture.
type FoundFace struct {
	ID             int64
	X1, Y1, X2, Y2 float64
	Score          float64
	// Vector is the list of numbers of the face, or nil.
	Vector    []float32
	Dismissed bool
}

// FaceJob is a picture that the server must search for faces.
type FaceJob struct {
	Handle, ID, Path, Checksum string
}

// faceRetry is the time after which a failed search runs again, in seconds.
const faceRetry = 24 * 60 * 60

// FaceJobs returns pictures that have no search yet, or whose file changed.
// With vectors it also returns the pictures that were searched without the
// recognition model. It first forgets the faces of pictures that are gone.
func (s *Store) FaceJobs(ctx context.Context, vectors bool, limit int) ([]FaceJob, error) {
	for _, table := range []string{"face", "face_scan"} {
		// The table names come from the constant list above.
		if _, err := s.w.ExecContext(ctx, "DELETE FROM "+table+" WHERE media NOT IN (SELECT handle FROM media_index)"); err != nil { // #nosec G202
			return nil, err
		}
	}
	rows, err := s.r.QueryContext(ctx, `SELECT m.handle, m.gid, m.path, m.checksum, m.mime FROM media_index m
		LEFT JOIN face_scan f ON f.media = m.handle
		WHERE f.media IS NULL OR f.path <> m.path OR f.checksum <> m.checksum
		   OR (? AND f.vectors = 0 AND f.error = '') OR (f.error <> '' AND f.at < ?)
		ORDER BY m.gid`, vectors, s.now().Unix()-faceRetry)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FaceJob
	for rows.Next() && len(out) < limit {
		var job FaceJob
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

func encodeVector(vector []float32) []byte {
	if len(vector) == 0 {
		return nil
	}
	out := make([]byte, len(vector)*4)
	for i, v := range vector {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}
	return out
}

func decodeVector(data []byte) []float32 {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil
	}
	out := make([]float32, len(data)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return out
}

// faceOverlap returns the share of the common area of two boxes in their joint area.
func faceOverlap(ax1, ay1, ax2, ay2, bx1, by1, bx2, by2 float64) float64 {
	w := math.Min(ax2, bx2) - math.Max(ax1, bx1)
	h := math.Min(ay2, by2) - math.Max(ay1, by1)
	if w <= 0 || h <= 0 {
		return 0
	}
	inter := w * h
	return inter / ((ax2-ax1)*(ay2-ay1) + (bx2-bx1)*(by2-by1) - inter)
}

// SaveFaces stores the result of the search of one picture. width and
// height are the size of the stored picture. A face that an editor
// dismissed before stays dismissed. errText records a search that failed.
func (s *Store) SaveFaces(ctx context.Context, job FaceJob, width, height int, vectors bool, faces []FoundFace, errText string) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The picture can leave the tree while the search runs.
	var present int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM media_index WHERE handle = ?`, job.Handle).Scan(&present); err != nil {
		return err
	}
	if present == 0 {
		return nil
	}
	old, err := facesOf(ctx, tx, job.Handle)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM face WHERE media = ?`, job.Handle); err != nil {
		return err
	}
	for _, face := range faces {
		dismissed := slices.ContainsFunc(old, func(o FoundFace) bool {
			return o.Dismissed && faceOverlap(o.X1, o.Y1, o.X2, o.Y2, face.X1, face.Y1, face.X2, face.Y2) > 0.5
		})
		if _, err := tx.ExecContext(ctx, `INSERT INTO face (media, x1, y1, x2, y2, score, vector, dismissed) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			job.Handle, face.X1, face.Y1, face.X2, face.Y2, face.Score, encodeVector(face.Vector), dismissed); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO face_scan (media, path, checksum, width, height, vectors, at, error) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (media) DO UPDATE SET path = excluded.path, checksum = excluded.checksum, width = excluded.width,
			height = excluded.height, vectors = excluded.vectors, at = excluded.at, error = excluded.error`,
		job.Handle, job.Path, job.Checksum, width, height, vectors, s.now().Unix(), errText); err != nil {
		return err
	}
	return tx.Commit()
}

func facesOf(ctx context.Context, q querier, media string) ([]FoundFace, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, x1, y1, x2, y2, score, vector, dismissed FROM face WHERE media = ? ORDER BY x1, id`, media)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []FoundFace
	for rows.Next() {
		var face FoundFace
		var vector []byte
		if err := rows.Scan(&face.ID, &face.X1, &face.Y1, &face.X2, &face.Y2, &face.Score, &vector, &face.Dismissed); err != nil {
			return nil, err
		}
		face.Vector = decodeVector(vector)
		out = append(out, face)
	}
	return out, rows.Err()
}

// DismissFace records that a found face of a picture needs no mark. It
// reports false if the picture has no such face.
func (s *Store) DismissFace(ctx context.Context, mediaID string, face int64) (bool, error) {
	result, err := s.w.ExecContext(ctx, `UPDATE face SET dismissed = 1
		WHERE id = ? AND media = (SELECT handle FROM media_index WHERE gid = ?)`, face, mediaID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// faceMark is a mark of a person on a picture.
type faceMark struct {
	person string
	region *model.Region
}

// faceMarks returns the marks of all people, by the handle of the picture.
func (v *View) faceMarks(ctx context.Context) (map[string][]faceMark, error) {
	marks := map[string][]faceMark{}
	people, err := v.db.QueryContext(ctx, `SELECT handle, doc FROM object WHERE kind = 'person' ORDER BY gid`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = people.Close() }()
	for people.Next() {
		var handle, doc string
		if err := people.Scan(&handle, &doc); err != nil {
			return nil, err
		}
		if !strings.Contains(doc, `"mediaRefs"`) {
			continue
		}
		var person struct {
			MediaRefs []model.MediaRef `json:"mediaRefs"`
		}
		if err := json.Unmarshal([]byte(doc), &person); err != nil {
			return nil, err
		}
		for _, ref := range person.MediaRefs {
			if v.sees(ref.Private) {
				marks[ref.HLink] = append(marks[ref.HLink], faceMark{person: handle, region: ref.Region})
			}
		}
	}
	return marks, people.Err()
}

// marked reports whether a mark shows this face: the two boxes share a good
// part of their area, or each holds the middle of the other.
func marked(region *model.Region, face FoundFace) bool {
	if region == nil {
		return false
	}
	x1, y1, x2, y2 := float64(region.X1), float64(region.Y1), float64(region.X2), float64(region.Y2)
	if faceOverlap(x1, y1, x2, y2, face.X1, face.Y1, face.X2, face.Y2) >= 0.2 {
		return true
	}
	fx, fy := (face.X1+face.X2)/2, (face.Y1+face.Y2)/2
	mx, my := (x1+x2)/2, (y1+y2)/2
	return fx > x1 && fx < x2 && fy > y1 && fy < y2 && mx > face.X1 && mx < face.X2 && my > face.Y1 && my < face.Y2
}

// SuggestedFace is a face on a picture that has no mark yet.
type SuggestedFace struct {
	ID int64 `json:"id"`
	// Region is a square around the face with some room, for a mark.
	Region model.Region `json:"region"`
	// People are the people whose marked faces are like this face, the most alike first.
	People []*Card `json:"people"`
}

// PhotoFaces says what the server found on one picture.
type PhotoFaces struct {
	// Scanned is false while the server did not search the picture yet.
	Scanned bool            `json:"scanned"`
	Faces   []SuggestedFace `json:"faces"`
}

const (
	// faceLikeness is the lowest likeness of two faces for a suggestion. It
	// is a little above the value that the authors of the model give for
	// "the same person", because relatives look alike.
	faceLikeness = 0.38
	// faceCandidates is the number of people that one face can suggest.
	faceCandidates = 3
	// faceRoom makes the square of a mark larger than the found face.
	faceRoom = 1.5
)

func likeness(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

// markRegion returns a square around a found face, in whole percent of the
// stored picture. The square is a square in pixels.
func markRegion(face FoundFace, width, height int) model.Region {
	w, h := float64(max(width, 1)), float64(max(height, 1))
	// The sides of the face in pixels.
	side := math.Max((face.X2-face.X1)/100*w, (face.Y2-face.Y1)/100*h) * faceRoom
	side = math.Min(side, math.Min(w, h))
	cx, cy := (face.X1+face.X2)/200*w, (face.Y1+face.Y2)/200*h
	left := math.Min(math.Max(cx-side/2, 0), w-side)
	top := math.Min(math.Max(cy-side/2, 0), h-side)
	region := model.Region{
		X1: int(math.Round(left / w * 100)), Y1: int(math.Round(top / h * 100)),
		X2: int(math.Round((left + side) / w * 100)), Y2: int(math.Round((top + side) / h * 100)),
	}
	region.X2, region.Y2 = min(max(region.X2, region.X1+1), 100), min(max(region.Y2, region.Y1+1), 100)
	region.X1, region.Y1 = min(region.X1, region.X2-1), min(region.Y1, region.Y2-1)
	return region
}

// FoundFaces returns the faces of one picture that have no mark and that no
// editor dismissed. If the server can compare faces, each face names the
// people whose marked faces on all pictures are like it.
func (v *View) FoundFaces(ctx context.Context, mediaID string) (*PhotoFaces, error) {
	row, err := scanMedia(v.db.QueryRowContext(ctx, `SELECT `+mediaColumns+` FROM media_index WHERE gid = ?`, mediaID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !v.allows(row.private, row.living) || !IsImage(row.mime) {
		return nil, ErrNotFound
	}
	var handle string
	if err := v.db.QueryRowContext(ctx, `SELECT handle FROM media_index WHERE gid = ?`, mediaID).Scan(&handle); err != nil {
		return nil, err
	}
	out := &PhotoFaces{Faces: []SuggestedFace{}}
	var width, height int
	var scanError string
	err = v.db.QueryRowContext(ctx, `SELECT width, height, error FROM face_scan WHERE media = ?`, handle).Scan(&width, &height, &scanError)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.Scanned = true
	marks, err := v.faceMarks(ctx)
	if err != nil {
		return nil, err
	}
	here, err := facesOf(ctx, v.db, handle)
	if err != nil {
		return nil, err
	}
	var open []FoundFace
	for _, face := range here {
		if face.Dismissed || slices.ContainsFunc(marks[handle], func(m faceMark) bool { return marked(m.region, face) }) {
			continue
		}
		open = append(open, face)
	}
	if len(open) == 0 {
		return out, nil
	}

	// The known faces: the found faces of all pictures that have a mark.
	type known struct {
		person string
		vector []float32
	}
	var knownFaces []known
	if slices.ContainsFunc(open, func(f FoundFace) bool { return f.Vector != nil }) {
		rows, err := v.db.QueryContext(ctx, `SELECT media, x1, y1, x2, y2, vector FROM face WHERE vector IS NOT NULL AND dismissed = 0`)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var media string
			var face FoundFace
			var vector []byte
			if err := rows.Scan(&media, &face.X1, &face.Y1, &face.X2, &face.Y2, &vector); err != nil {
				return nil, err
			}
			for _, mark := range marks[media] {
				if marked(mark.region, face) {
					knownFaces = append(knownFaces, known{person: mark.person, vector: decodeVector(vector)})
					break
				}
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	// A person who has a mark on this picture is not on it a second time.
	onPhoto := map[string]bool{}
	for _, mark := range marks[handle] {
		onPhoto[mark.person] = true
	}
	for _, face := range open {
		item := SuggestedFace{ID: face.ID, Region: markRegion(face, width, height), People: []*Card{}}
		best := map[string]float64{}
		for _, k := range knownFaces {
			if onPhoto[k.person] {
				continue
			}
			if l := likeness(face.Vector, k.vector); l >= faceLikeness && l > best[k.person] {
				best[k.person] = l
			}
		}
		people := make([]string, 0, len(best))
		for person := range best {
			people = append(people, person)
		}
		slices.SortFunc(people, func(a, b string) int {
			if best[a] != best[b] {
				if best[a] > best[b] {
					return -1
				}
				return 1
			}
			return strings.Compare(a, b)
		})
		for _, person := range people {
			if len(item.People) == faceCandidates {
				break
			}
			card, err := v.card(ctx, person)
			if err != nil {
				return nil, err
			}
			if card != nil {
				item.People = append(item.People, card)
			}
		}
		out.Faces = append(out.Faces, item)
	}
	return out, nil
}
