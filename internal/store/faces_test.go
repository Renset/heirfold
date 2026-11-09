package store

import (
	"context"
	"math"
	"testing"

	"github.com/Renset/heirfold/internal/model"
	"github.com/Renset/heirfold/internal/privacy"
)

// unit returns a list of numbers of the length 1 that points mostly in one direction.
func unit(direction int, side float32) []float32 {
	v := make([]float32, 128)
	v[direction] = 1
	v[(direction+1)%128] = side
	var length float64
	for _, x := range v {
		length += float64(x) * float64(x)
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / math.Sqrt(length))
	}
	return v
}

func faceJobs(t *testing.T, st *Store, vectors bool) []FaceJob {
	t.Helper()
	jobs, err := st.FaceJobs(context.Background(), vectors, 100)
	if err != nil {
		t.Fatal(err)
	}
	return jobs
}

func saveFaces(t *testing.T, st *Store, job FaceJob, vectors bool, faces ...FoundFace) {
	t.Helper()
	if err := st.SaveFaces(context.Background(), job, 1000, 500, vectors, faces, ""); err != nil {
		t.Fatal(err)
	}
}

func foundFaces(t *testing.T, st *Store, id string) *PhotoFaces {
	t.Helper()
	found, err := st.View(privacy.Full).FoundFaces(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func jobFor(t *testing.T, st *Store, id string) FaceJob {
	t.Helper()
	for _, job := range faceJobs(t, st, true) {
		if job.ID == id {
			return job
		}
	}
	t.Fatalf("no job for %s", id)
	return FaceJob{}
}

func TestFaceJobsFollowThePictures(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	first := addGalleryPhoto(t, st, "a.jpg")
	second := addGalleryPhoto(t, st, "b.jpg")
	jobs := faceJobs(t, st, false)
	// The old picture of the tree and the two new photos.
	if len(jobs) != 3 || jobs[0].ID != "O0001" || jobs[1].Path != "heirfold/a.jpg" {
		t.Fatalf("jobs: %+v", jobs)
	}
	saveFaces(t, st, jobs[1], false, FoundFace{X1: 10, Y1: 10, X2: 20, Y2: 30, Score: 0.9})
	if err := st.SaveFaces(context.Background(), jobs[0], 0, 0, false, nil, "the file is missing"); err != nil {
		t.Fatal(err)
	}
	// A searched picture is done. A failed search waits a day.
	if left := faceJobs(t, st, false); len(left) != 1 || left[0].ID != second {
		t.Fatalf("jobs after two searches: %+v", left)
	}
	// With the recognition model the picture without numbers comes back.
	if left := faceJobs(t, st, true); len(left) != 2 || left[0].ID != first {
		t.Fatalf("jobs with the model: %+v", left)
	}
	for range 24*60*60 + 1 {
		tick()
	}
	if left := faceJobs(t, st, false); len(left) != 2 || left[0].ID != "O0001" {
		t.Fatalf("jobs after a day: %+v", left)
	}
	// A removed photo takes its faces with it.
	mustEdit(t, st, "gallery.remove", func(e *Edit) error { return e.RemoveGalleryPhoto(first) })
	faceJobs(t, st, false)
	var faces, scans int
	if err := st.r.QueryRow(`SELECT (SELECT count(*) FROM face), (SELECT count(*) FROM face_scan)`).Scan(&faces, &scans); err != nil {
		t.Fatal(err)
	}
	if faces != 0 || scans != 1 {
		t.Fatalf("%d faces and %d searches after the removal", faces, scans)
	}
	// A search that ends after the removal stores nothing.
	saveFaces(t, st, jobs[1], false, FoundFace{X1: 10, Y1: 10, X2: 20, Y2: 30, Score: 0.9})
	if err := st.r.QueryRow(`SELECT count(*) FROM face`).Scan(&faces); err != nil || faces != 0 {
		t.Fatalf("%d faces for a removed photo: %v", faces, err)
	}
}

func TestFoundFacesSuggestPeople(t *testing.T) {
	st, tick := editStore(t, editTree())
	tick()
	known := addGalleryPhoto(t, st, "known.jpg")
	other := addGalleryPhoto(t, st, "other.jpg")
	fresh := addGalleryPhoto(t, st, "new.jpg")

	// Before a search the picture says so.
	if got := foundFaces(t, st, fresh); got.Scanned || len(got.Faces) != 0 {
		t.Fatalf("before the search: %+v", got)
	}
	// The first photo has two faces. An editor marks Ivan and Maria on them.
	saveFaces(t, st, jobFor(t, st, known), true,
		FoundFace{X1: 10, Y1: 20, X2: 20, Y2: 40, Score: 0.9, Vector: unit(0, 0.1)},
		FoundFace{X1: 60, Y1: 20, X2: 70, Y2: 40, Score: 0.9, Vector: unit(5, 0.1)})
	if got := foundFaces(t, st, known); !got.Scanned || len(got.Faces) != 2 || len(got.Faces[0].People) != 0 {
		t.Fatalf("faces before the marks: %+v", got)
	}
	// The square of a suggestion is a square in pixels, around the face, inside the picture.
	region := foundFaces(t, st, known).Faces[0].Region
	if w, h := (region.X2-region.X1)*1000, (region.Y2-region.Y1)*500; math.Abs(float64(w-h)) > 1000 ||
		region.X1 > 10 || region.X2 < 20 || region.Y1 > 20 || region.Y2 < 40 || region.X1 < 0 || region.Y2 > 100 {
		t.Fatalf("region: %+v", region)
	}
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(known, "I0001", region) })
	mustEdit(t, st, "gallery.mark", func(e *Edit) error {
		return e.MarkFace(known, "I0002", model.Region{X1: 58, Y1: 15, X2: 73, Y2: 45})
	})
	if got := foundFaces(t, st, known); len(got.Faces) != 0 {
		t.Fatalf("marked faces are still open: %+v", got)
	}

	// A second photo of Ivan gives one more known face.
	saveFaces(t, st, jobFor(t, st, other), true, FoundFace{X1: 40, Y1: 40, X2: 60, Y2: 80, Score: 0.8, Vector: unit(0, 0.3)})
	if got := foundFaces(t, st, other); len(got.Faces) != 1 || len(got.Faces[0].People) != 1 || got.Faces[0].People[0].ID != "I0001" {
		t.Fatalf("suggestion on the second photo: %+v", got.Faces)
	}

	// The new photo: a face like Ivan, a face like Maria and Ivan, an unknown face, a face without numbers.
	mixed := unit(5, 0.9)
	saveFaces(t, st, jobFor(t, st, fresh), true,
		FoundFace{X1: 5, Y1: 10, X2: 15, Y2: 30, Score: 0.9, Vector: unit(0, 0.2)},
		FoundFace{X1: 30, Y1: 10, X2: 40, Y2: 30, Score: 0.9, Vector: mixed},
		FoundFace{X1: 55, Y1: 10, X2: 65, Y2: 30, Score: 0.9, Vector: unit(50, 0)},
		FoundFace{X1: 80, Y1: 10, X2: 90, Y2: 30, Score: 0.9})
	got := foundFaces(t, st, fresh)
	if len(got.Faces) != 4 {
		t.Fatalf("faces: %+v", got.Faces)
	}
	names := func(face SuggestedFace) string {
		out := ""
		for _, card := range face.People {
			out += card.ID + " "
		}
		return out
	}
	if names(got.Faces[0]) != "I0001 " || names(got.Faces[1]) != "I0002 " || names(got.Faces[2]) != "" || names(got.Faces[3]) != "" {
		t.Fatalf("suggestions: %q %q %q %q", names(got.Faces[0]), names(got.Faces[1]), names(got.Faces[2]), names(got.Faces[3]))
	}

	// A mark of Ivan on the new photo takes him out of the other suggestions of this photo.
	mustEdit(t, st, "gallery.mark", func(e *Edit) error { return e.MarkFace(fresh, "I0001", got.Faces[0].Region) })
	after := foundFaces(t, st, fresh)
	if len(after.Faces) != 3 || names(after.Faces[0]) != "I0002 " {
		t.Fatalf("after the mark: %+v", after.Faces)
	}

	// A dismissed face goes away and stays away after a new search.
	if ok, err := st.DismissFace(context.Background(), fresh, after.Faces[1].ID); err != nil || !ok {
		t.Fatalf("dismiss: %v %v", ok, err)
	}
	if ok, err := st.DismissFace(context.Background(), known, after.Faces[2].ID); err != nil || ok {
		t.Fatalf("dismiss with the wrong photo: %v %v", ok, err)
	}
	if left := foundFaces(t, st, fresh); len(left.Faces) != 2 {
		t.Fatalf("after the dismissal: %+v", left.Faces)
	}
	job := FaceJob{}
	if err := st.r.QueryRow(`SELECT handle, gid, path, checksum FROM media_index WHERE gid = ?`, fresh).Scan(&job.Handle, &job.ID, &job.Path, &job.Checksum); err != nil {
		t.Fatal(err)
	}
	saveFaces(t, st, job, true,
		FoundFace{X1: 5, Y1: 10, X2: 15, Y2: 30, Score: 0.9, Vector: unit(0, 0.2)},
		FoundFace{X1: 30, Y1: 10, X2: 40, Y2: 30, Score: 0.9, Vector: mixed},
		FoundFace{X1: 55.5, Y1: 10, X2: 65, Y2: 30.5, Score: 0.9, Vector: unit(50, 0)},
		FoundFace{X1: 80, Y1: 10, X2: 90, Y2: 30, Score: 0.9})
	if left := foundFaces(t, st, fresh); len(left.Faces) != 2 {
		t.Fatalf("after a new search: %+v", left.Faces)
	}

	// A guest gets nothing: the photos are hidden for the guest.
	if _, err := st.View(privacy.Public).FoundFaces(context.Background(), fresh); err != ErrNotFound {
		t.Fatalf("a guest: %v", err)
	}
	if _, err := st.View(privacy.Full).FoundFaces(context.Background(), "O9999"); err != ErrNotFound {
		t.Fatalf("an unknown photo: %v", err)
	}
}
