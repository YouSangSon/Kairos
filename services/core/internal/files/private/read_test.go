package privatefile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadBoundedUsesActualRegularDescriptor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "regular")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if raw, err := ReadBounded(path, 3); err != nil || string(raw) != "abc" {
		t.Fatalf("bounded file rejected: %v", err)
	}
	if _, err := ReadBounded(path, 2); err == nil {
		t.Fatal("byte limit ignored")
	}
	if _, err := ReadBounded(dir, 3); err == nil {
		t.Fatal("directory accepted")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBounded(fifo, 3); err == nil {
		t.Fatal("FIFO accepted")
	}
}
