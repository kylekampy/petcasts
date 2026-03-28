package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewLocal(dir)

	data := []byte("hello petcast")
	path, err := store.Save("test/file.txt", data)
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	if path != filepath.Join(dir, "test/file.txt") {
		t.Errorf("Save() path = %q, want %q", path, filepath.Join(dir, "test/file.txt"))
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file doesn't exist: %v", err)
	}

	loaded, err := store.Load("test/file.txt")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if string(loaded) != "hello petcast" {
		t.Errorf("Load() = %q, want %q", string(loaded), "hello petcast")
	}
}

func TestLocalExists(t *testing.T) {
	dir := t.TempDir()
	store := NewLocal(dir)

	if store.Exists("nope.txt") {
		t.Error("Exists() for missing file should be false")
	}

	store.Save("exists.txt", []byte("yes"))
	if !store.Exists("exists.txt") {
		t.Error("Exists() for saved file should be true")
	}
}

func TestLocalFullPath(t *testing.T) {
	store := NewLocal("/data")
	if got := store.FullPath("a/b.png"); got != "/data/a/b.png" {
		t.Errorf("FullPath() = %q, want %q", got, "/data/a/b.png")
	}
}

func TestLocalPathTraversal(t *testing.T) {
	dir := t.TempDir()
	store := NewLocal(dir)

	// Attempts to escape the root should fail
	_, err := store.Save("../../etc/passwd", []byte("evil"))
	if err == nil {
		t.Error("Save() should reject path traversal")
	}

	_, err = store.Load("../../../etc/passwd")
	if err == nil {
		t.Error("Load() should reject path traversal")
	}

	if store.Exists("../../etc/passwd") {
		t.Error("Exists() should reject path traversal")
	}
}

func TestLocalCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	store := NewLocal(dir)

	_, err := store.Save("deep/nested/dir/file.txt", []byte("deep"))
	if err != nil {
		t.Fatalf("Save() with nested dirs: %v", err)
	}

	data, err := store.Load("deep/nested/dir/file.txt")
	if err != nil {
		t.Fatalf("Load() nested: %v", err)
	}
	if string(data) != "deep" {
		t.Errorf("Load() = %q, want %q", string(data), "deep")
	}
}
