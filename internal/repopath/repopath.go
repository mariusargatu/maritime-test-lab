// Package repopath resolves paths relative to the repository root (the directory
// containing go.mod), so tests and fixtures can reach committed files regardless
// of which package directory the test runs from. One small helper replaces the
// hand-rolled go.mod walk-up that several test packages used to each carry.
package repopath

import (
	"fmt"
	"os"
	"path/filepath"
)

// Root returns the repository root: the nearest ancestor of the working
// directory that contains a go.mod file.
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repopath: go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// Find resolves rel against the repository root.
func Find(rel string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, rel), nil
}

// Read reads the repo-relative file at rel.
func Read(rel string) ([]byte, error) {
	p, err := Find(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}
