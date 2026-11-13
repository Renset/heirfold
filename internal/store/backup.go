package store

import (
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Renset/heirfold/internal/gramps"
)

// backupsKept is the number of automatic copies that stay on disk.
const backupsKept = 10

// Backup writes the whole tree as a Gramps XML file into the directory and
// returns the path of the file. Heirfold calls it before it replaces a tree,
// so a wrong import or a wrong start option loses nothing. The directory
// keeps the newest files only.
func (s *Store) Backup(ctx context.Context, dir string) (string, error) {
	tree, err := s.ExportTree(ctx)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	base := "tree-" + s.now().UTC().Format("20060102-150405")
	var path string
	var file *os.File
	// Two copies in one second get the numbers 2, 3 and so on.
	for n := 1; ; n++ {
		path = filepath.Join(dir, base+".gramps")
		if n > 1 {
			path = filepath.Join(dir, base+"-"+strconv.Itoa(n)+".gramps")
		}
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- the directory is inside the data directory.
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) || n >= 100 {
			return "", err
		}
	}
	gz := gzip.NewWriter(file)
	if err := gramps.Write(gz, tree); err != nil {
		_ = gz.Close()
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := gz.Close(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return path, nil //nolint:nilerr // the copy is written; only the removal of old copies waits.
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "tree-") && strings.HasSuffix(entry.Name(), ".gramps") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for len(names) > backupsKept {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
	return path, nil
}
