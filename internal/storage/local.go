package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Local stores files on the local filesystem under a root directory.
type Local struct {
	Root string
}

func NewLocal(root string) *Local {
	return &Local{Root: root}
}

// safePath validates that a relative path doesn't escape the storage root.
func (l *Local) safePath(relPath string) (string, error) {
	full := filepath.Join(l.Root, relPath)
	absRoot, _ := filepath.Abs(l.Root)
	absFull, _ := filepath.Abs(full)
	if !strings.HasPrefix(absFull, absRoot+string(os.PathSeparator)) && absFull != absRoot {
		return "", fmt.Errorf("path escapes storage root: %s", relPath)
	}
	return full, nil
}

// Save writes data to the given relative path under the storage root.
func (l *Local) Save(relPath string, data []byte) (string, error) {
	full, err := l.safePath(relPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", err
	}
	return full, nil
}

// SaveReader writes from a reader to the given relative path.
func (l *Local) SaveReader(relPath string, r io.Reader) (string, error) {
	full, err := l.safePath(relPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(full)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return "", err
	}
	return full, nil
}

// Load reads a file by relative path.
func (l *Local) Load(relPath string) ([]byte, error) {
	full, err := l.safePath(relPath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

// FullPath returns the absolute path for a relative storage path.
func (l *Local) FullPath(relPath string) string {
	return filepath.Join(l.Root, relPath)
}

// Exists checks if a file exists at the relative path.
func (l *Local) Exists(relPath string) bool {
	_, err := os.Stat(filepath.Join(l.Root, relPath))
	return err == nil
}
