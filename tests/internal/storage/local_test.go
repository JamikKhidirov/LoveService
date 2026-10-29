// Package storage_test contains black-box tests for internal/storage.
package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/loveservice/loveservice/internal/storage"
)

func TestLocalPutReadDelete(t *testing.T) {
	dir := t.TempDir()
	l, err := storage.NewLocal(dir, "http://media.test/base")
	if err != nil {
		t.Fatal(err)
	}

	url, err := l.Put(context.Background(), "users/1/photo.txt", strings.NewReader("hello"), 5, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://media.test/base/users/1/photo.txt" {
		t.Fatalf("unexpected url %q", url)
	}
	body, err := os.ReadFile(filepath.Join(dir, "users", "1", "photo.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Fatalf("stored body = %q", body)
	}

	if err := l.Delete(context.Background(), "users/1/photo.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "users", "1", "photo.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, err=%v", err)
	}

	// Deleting a missing object is idempotent.
	if err := l.Delete(context.Background(), "users/1/photo.txt"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestLocalRejectsTraversal(t *testing.T) {
	l, err := storage.NewLocal(t.TempDir(), "http://media.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Put(context.Background(), "../escape.txt", strings.NewReader("x"), 1, "text/plain"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	if _, err := l.Put(context.Background(), "", strings.NewReader("x"), 1, "text/plain"); err == nil {
		t.Fatal("expected empty key to be rejected")
	}
}

func TestLocalURLNormalisesSlashes(t *testing.T) {
	l, err := storage.NewLocal(t.TempDir(), "http://media.test/")
	if err != nil {
		t.Fatal(err)
	}
	if got := l.URL("/a/b.txt"); got != "http://media.test/a/b.txt" {
		t.Fatalf("URL = %q", got)
	}
}
