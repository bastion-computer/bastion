// Package actions embeds built-in preset actions and seeds them into the data directory.
package actions

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DirName is the data-directory subdirectory where preset actions are stored.
const DirName = "actions"

//go:embed setup_node setup_mise
var files embed.FS

// Seed copies missing built-in preset actions into dataDir/actions.
func Seed(dataDir string) error {
	if dataDir == "" {
		return errors.New("data dir is required")
	}

	if err := requireExistingDir(dataDir); err != nil {
		return err
	}

	actionsDir := filepath.Join(dataDir, DirName)
	if err := os.Mkdir(actionsDir, 0o750); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create actions data directory: %w", err)
		}

		info, statErr := os.Stat(actionsDir)
		if statErr != nil {
			return fmt.Errorf("stat actions data directory: %w", statErr)
		}

		if !info.IsDir() {
			return fmt.Errorf("actions data path %s is not a directory", actionsDir)
		}
	}

	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return fmt.Errorf("read embedded actions: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dst := filepath.Join(actionsDir, entry.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat preset action %s: %w", entry.Name(), err)
		}

		if err := copyEmbeddedDir(entry.Name(), dst); err != nil {
			_ = os.RemoveAll(dst)

			return err
		}
	}

	return nil
}

func requireExistingDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat data directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("data directory %s is not a directory", path)
	}

	return nil
}

func copyEmbeddedDir(src, dst string) error {
	return fs.WalkDir(files, src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, filepath.FromSlash(rel))
		if rel == "." {
			target = dst
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if entry.IsDir() {
			mode := info.Mode().Perm()
			if mode == 0 {
				mode = 0o750
			} else {
				mode |= 0o700
			}

			return os.MkdirAll(target, mode)
		}

		if !info.Mode().IsRegular() {
			return fmt.Errorf("embedded action file %s is not regular", path)
		}

		contents, err := files.ReadFile(path)
		if err != nil {
			return err
		}

		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o640
		} else {
			mode |= 0o600
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}

		return os.WriteFile(target, contents, mode)
	})
}
