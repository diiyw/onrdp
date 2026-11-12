package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// projectFiles reads the real resource tree from disk so tests exercise the same
// page, icons and project metadata the app ships with.
func projectFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	source := fstest.MapFS{}
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel("../..", path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		// Test files are not part of the shipped project source.
		if strings.HasSuffix(relative, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source[relative] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		t.Fatalf("walk resources: %v", err)
	}
	if len(source) == 0 {
		t.Fatal("no project files found")
	}
	return source
}
