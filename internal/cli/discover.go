package cli

import (
	"os"
	"path/filepath"
)

const dataDirName = ".ssearch"

// FindOrCreateDataDir implements the upward-search algorithm per PRD §2.1:
//
//  1. Walk up from cwd looking for a directory named .ssearch.
//  2. If found, its parent is the project root; the .ssearch dir is the data dir.
//  3. If not found (reached filesystem root), create .ssearch in cwd;
//     cwd becomes the project root.
func FindOrCreateDataDir(cwd string) (projectRoot, dataDir string, err error) {
	projectRoot, dataDir, err = findUpward(cwd)
	if err == nil {
		return projectRoot, dataDir, nil
	}
	if !os.IsNotExist(err) {
		return "", "", err
	}
	// Not found: create in cwd.
	dataDir = filepath.Join(cwd, dataDirName)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", "", err
	}
	return cwd, dataDir, nil
}

// findUpward walks directory parents looking for .ssearch.
func findUpward(dir string) (projectRoot, dataDir string, err error) {
	for {
		candidate := filepath.Join(dir, dataDirName)
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return dir, candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			return "", "", os.ErrNotExist
		}
		dir = parent
	}
}
