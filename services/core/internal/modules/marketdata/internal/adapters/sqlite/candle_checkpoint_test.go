package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
)

func TestCandleCheckpointCommitFailurePreservesPrefix(t *testing.T) {
	ctx := t.Context()
	work := privateCandleCheckpointTestWork(t)
	checkpoint := openTestCandleCheckpoint(t, work)
	first := testCandleCheckpointPage(1, false)
	next := testCandleCheckpointPage(2, true)
	if err := checkpoint.Append(ctx, first); err != nil {
		t.Fatal(err)
	}
	if concurrent, err := OpenCandleCheckpoint(ctx, work, "005930", "1d"); err == nil {
		concurrent.Close()
		t.Fatal("another writer acquired the held directory")
	}

	reader := openCandleCheckpointFixture(t, filepath.Join(work, CandleCheckpointFile), "ro")
	read, err := reader.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback()
	var count int
	if err := read.QueryRowContext(ctx, "SELECT count(*) FROM candle_capture_pages").Scan(&count); err != nil || count != 1 {
		t.Fatalf("acquire real reader lock: rows=%d error=%v", count, err)
	}
	var conflict sqlite3.Error
	if err := checkpoint.Append(ctx, next); !errors.As(err, &conflict) || conflict.Code != sqlite3.ErrBusy {
		t.Fatalf("expected SQLite to reject COMMIT under reader lock, got %v", err)
	}
	if err := read.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatal(err)
	}
	checkpoint = openTestCandleCheckpoint(t, work)
	assertCandleCheckpointPages(t, checkpoint, first)
	for _, bad := range []CandleCheckpointPage{first, testCandleCheckpointPage(3, false), {
		Number: next.Number, Digest: next.Digest, StartedAt: next.StartedAt.Add(time.Nanosecond), Payload: next.Payload,
	}} {
		if err := checkpoint.Append(ctx, bad); err == nil {
			t.Fatalf("accepted conflicting checkpoint page %d", bad.Number)
		}
	}
	if err := checkpoint.Append(ctx, next); err != nil {
		t.Fatal(err)
	}
	assertCandleCheckpointPages(t, checkpoint, first, next)
	if err := checkpoint.Append(ctx, testCandleCheckpointPage(3, false)); err == nil {
		t.Fatal("advanced a completed checkpoint")
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatal(err)
	}
	for _, scope := range [][2]string{{"000660", "1d"}, {"005930", "5m"}} {
		if other, err := OpenCandleCheckpoint(ctx, work, scope[0], scope[1]); err == nil {
			other.Close()
			t.Fatalf("reopened checkpoint in another scope %v", scope)
		}
	}
	checkpoint = openTestCandleCheckpoint(t, work)
	assertCandleCheckpointPages(t, checkpoint, first, next)
}

func TestCandleCheckpointProtectsExistingFiles(t *testing.T) {
	t.Run("foreign database with copied application ID", func(t *testing.T) {
		work := privateCandleCheckpointTestWork(t)
		path := filepath.Join(work, CandleCheckpointFile)
		db := openCandleCheckpointFixture(t, path, "rwc")
		if _, err := db.Exec("CREATE TABLE other_data (keep TEXT); INSERT INTO other_data VALUES ('preserve me'); PRAGMA application_id = 1262699344"); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		assertCandleCheckpointRefused(t, work, path)
	})
	t.Run("empty reserved database file", func(t *testing.T) {
		work := privateCandleCheckpointTestWork(t)
		path := filepath.Join(work, CandleCheckpointFile)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		assertCandleCheckpointRefused(t, work, path)
	})
	t.Run("unknown journal beside valid checkpoint", func(t *testing.T) {
		work := privateCandleCheckpointTestWork(t)
		checkpoint := openTestCandleCheckpoint(t, work)
		if err := checkpoint.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(work, CandleCheckpointFile)
		journal := path + "-journal"
		if err := os.WriteFile(journal, []byte("unrelated private data"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertCandleCheckpointRefused(t, work, path, journal)
	})
	t.Run("oversized database", func(t *testing.T) {
		work := privateCandleCheckpointTestWork(t)
		if err := openTestCandleCheckpoint(t, work).Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(work, CandleCheckpointFile)
		const size = maxCandleCheckpointFileBytes + 1
		if err := os.Truncate(path, size); err != nil {
			t.Fatal(err)
		}
		assertCandleCheckpointRefused(t, work)
		if info, err := os.Stat(path); err != nil || info.Size() != size {
			t.Fatalf("rejected database changed: %v, %v", info, err)
		}
	})
	t.Run("work directory has shared search permissions", func(t *testing.T) {
		work := privateCandleCheckpointTestWork(t)
		if err := os.Chmod(work, 0o750); err != nil {
			t.Fatal(err)
		}
		assertCandleCheckpointRefused(t, work)
	})
	t.Run("work directory is a symlink", func(t *testing.T) {
		work := privateCandleCheckpointTestWork(t)
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(work, alias); err != nil {
			t.Fatal(err)
		}
		assertCandleCheckpointRefused(t, alias)
		if _, err := os.Lstat(filepath.Join(work, CandleCheckpointFile)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("symlink target changed: %v", err)
		}
	})
	t.Run("work ancestor has shared write permissions", func(t *testing.T) {
		parent := t.TempDir()
		work := filepath.Join(parent, "work")
		if err := os.Mkdir(work, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(parent, 0o770); err != nil {
			t.Fatal(err)
		}
		assertCandleCheckpointRefused(t, work)
	})
	for _, target := range []string{CandleCheckpointFile, CandleCheckpointFile + "-journal"} {
		for _, alias := range []struct {
			name string
			link func(string, string) error
		}{
			{"symlink", os.Symlink},
			{"hard link", os.Link},
		} {
			t.Run(target+"/"+alias.name, func(t *testing.T) {
				work := privateCandleCheckpointTestWork(t)
				path := filepath.Join(work, CandleCheckpointFile)
				if target != CandleCheckpointFile {
					if err := openTestCandleCheckpoint(t, work).Close(); err != nil {
						t.Fatal(err)
					}
				}
				external := filepath.Join(t.TempDir(), "keep")
				if err := os.WriteFile(external, []byte("keep this file"), 0o600); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(work, target)
				if err := alias.link(external, link); err != nil {
					t.Fatal(err)
				}
				protected := []string{external, link}
				if target != CandleCheckpointFile {
					protected = append(protected, path)
				}
				assertCandleCheckpointRefused(t, work, protected...)
			})
		}
	}
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		t.Run("unused database with "+suffix, func(t *testing.T) {
			work := privateCandleCheckpointTestWork(t)
			path := filepath.Join(work, CandleCheckpointFile+suffix)
			if err := os.WriteFile(path, []byte("unrelated"), 0o600); err != nil {
				t.Fatal(err)
			}
			assertCandleCheckpointRefused(t, work, path)
			if _, err := os.Lstat(filepath.Join(work, CandleCheckpointFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("opened new database over a reserved sidecar: %v", err)
			}
		})
	}
}

func TestCandleCheckpointRestartsInterruptedInitialization(t *testing.T) {
	work := privateCandleCheckpointTestWork(t)
	prefix := "." + CandleCheckpointFile + ".init-"
	abandoned := filepath.Join(work, prefix+"unpublished")
	if err := os.WriteFile(abandoned, []byte("unidentified partial database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := openTestCandleCheckpoint(t, work).Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(work, CandleCheckpointFile)
	publishedStage := filepath.Join(work, prefix+"published")
	if err := os.Link(path, publishedStage); err != nil {
		t.Fatal(err)
	}
	checkpoint := openTestCandleCheckpoint(t, work)
	defer checkpoint.Close()
	assertCandleCheckpointPages(t, checkpoint)
	if _, err := os.Lstat(publishedStage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed initialization retained its staging link: %v", err)
	}
	raw, err := os.ReadFile(abandoned)
	if err != nil || string(raw) != "unidentified partial database" {
		t.Fatalf("unrecognized staging bytes changed: %q, %v", raw, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode() != 0o600 {
		t.Fatalf("final database permissions: %v, %v", info, err)
	}
}

func TestCandleCheckpointRecoversAnEmptyOwnedJournal(t *testing.T) {
	work := privateCandleCheckpointTestWork(t)
	checkpoint := openTestCandleCheckpoint(t, work)
	if err := checkpoint.beginWrite(); err != nil {
		t.Fatal(err)
	}
	journal := checkpoint.path + "-journal"
	if err := os.WriteFile(journal, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTestCandleCheckpoint(t, work)
	assertCandleCheckpointPages(t, reopened)
	requireNoCandleCheckpointWriteResidue(t, reopened)
	if err := reopened.Append(t.Context(), testCandleCheckpointPage(1, true)); err != nil {
		t.Fatal(err)
	}
}

func TestCandleCheckpointRollsBackARealHotJournal(t *testing.T) {
	const workEnv = "KAIROS_TEST_CANDLE_CHECKPOINT_WRITE_CRASH"
	if work := os.Getenv(workEnv); work != "" {
		checkpoint := openTestCandleCheckpoint(t, work)
		if err := checkpoint.beginWrite(); err != nil {
			t.Fatal(err)
		}
		if _, err := checkpoint.db.Exec("PRAGMA cache_size=1; PRAGMA cache_spill=1"); err != nil {
			t.Fatal(err)
		}
		tx, err := checkpoint.db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		page := testCandleCheckpointPage(2, true)
		page.Payload = bytes.Repeat([]byte("q"), 1<<19)
		start := page.StartedAt.Format(time.RFC3339Nano)
		if _, err := tx.Exec("INSERT INTO candle_capture_pages VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			page.Number, checkpoint.symbol, checkpoint.interval, start, page.Digest, page.Complete, page.Payload, checkpoint.pageChecksum(page, start)); err != nil {
			t.Fatal(err)
		}
		os.Exit(73) // Leave the real SQLite transaction and its journal open.
	}
	work := privateCandleCheckpointTestWork(t)
	checkpoint := openTestCandleCheckpoint(t, work)
	first := testCandleCheckpointPage(1, false)
	if err := checkpoint.Append(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCandleCheckpointRollsBackARealHotJournal$")
	cmd.Env = append(os.Environ(), workEnv+"="+work)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 73 {
		t.Fatalf("writer did not exit during its transaction: %v, %s", err, output)
	}
	journal, err := os.ReadFile(checkpoint.path + "-journal")
	magic := []byte{0xd9, 0xd5, 0x05, 0xf9, 0x20, 0xa1, 0x63, 0xd7}
	if err != nil || len(journal) <= 512 || !bytes.HasPrefix(journal, magic) {
		t.Fatalf("interrupted transaction did not leave a real hot journal: len=%d, error=%v", len(journal), err)
	}
	reopened := openTestCandleCheckpoint(t, work)
	assertCandleCheckpointPages(t, reopened, first)
	requireNoCandleCheckpointWriteResidue(t, reopened)
	next := testCandleCheckpointPage(2, true)
	if err := reopened.Append(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	assertCandleCheckpointPages(t, reopened, first, next)
}

func requireNoCandleCheckpointWriteResidue(t *testing.T, checkpoint *CandleCheckpoint) {
	t.Helper()
	for _, path := range []string{checkpoint.path + "-journal", checkpoint.pending} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("checkpoint retains write residue: %s, %v", path, err)
		}
	}
}

func TestCandleCheckpointRejectsCorruptedLatestPage(t *testing.T) {
	for _, field := range []struct {
		name  string
		query string
		value any
	}{
		{"payload", "UPDATE candle_capture_pages SET payload=? WHERE page_number=1", []byte("accidentally changed")},
		{"completion", "UPDATE candle_capture_pages SET complete=? WHERE page_number=1", true},
	} {
		t.Run(field.name, func(t *testing.T) {
			work := privateCandleCheckpointTestWork(t)
			checkpoint := openTestCandleCheckpoint(t, work)
			if err := checkpoint.Append(t.Context(), testCandleCheckpointPage(1, false)); err != nil {
				t.Fatal(err)
			}
			if err := checkpoint.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(work, CandleCheckpointFile)
			db := openCandleCheckpointFixture(t, path, "rw")
			if _, err := db.Exec(field.query, field.value); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			assertCandleCheckpointRefused(t, work, path)
		})
	}
}

func privateCandleCheckpointTestWork(t *testing.T) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	return work
}

func openTestCandleCheckpoint(t *testing.T, work string) *CandleCheckpoint {
	t.Helper()
	checkpoint, err := OpenCandleCheckpoint(t.Context(), work, "005930", "1d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { checkpoint.Close() })
	return checkpoint
}

func openCandleCheckpointFixture(t *testing.T, path, mode string) *sql.DB {
	t.Helper()
	dsn := url.URL{Scheme: "file", Path: path, RawQuery: "mode=" + mode + "&_busy_timeout=0"}
	db, err := sql.Open("sqlite3", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testCandleCheckpointPage(number int, complete bool) CandleCheckpointPage {
	return CandleCheckpointPage{
		Number: number, Digest: fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprint(number)))),
		StartedAt: time.Date(2026, time.November, 19, 0, 0, 0, 898, time.UTC),
		Complete:  complete, Payload: []byte(strings.Repeat("verified candle ", number)),
	}
}

func assertCandleCheckpointPages(t *testing.T, checkpoint *CandleCheckpoint, want ...CandleCheckpointPage) {
	t.Helper()
	pages, err := checkpoint.Pages(t.Context())
	if err != nil || len(pages) != len(want) {
		t.Fatalf("checkpoint pages=%d, want %d, error=%v", len(pages), len(want), err)
	}
	for i, expected := range want {
		payload := expected.Payload
		if i < len(want)-1 {
			payload = nil
		}
		got := pages[i]
		if got.Number != expected.Number || got.Digest != expected.Digest || !got.StartedAt.Equal(expected.StartedAt) || got.Complete != expected.Complete || !bytes.Equal(got.Payload, payload) {
			t.Fatalf("page %d does not match durable progress: %+v", i+1, got)
		}
	}
}

func assertCandleCheckpointRefused(t *testing.T, work string, paths ...string) {
	t.Helper()
	before := make([][]byte, len(paths))
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[i] = raw
	}
	if checkpoint, err := OpenCandleCheckpoint(context.Background(), work, "005930", "1d"); err == nil {
		checkpoint.Close()
		t.Fatal("opened unsafe candle checkpoint")
	}
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(raw, before[i]) {
			t.Fatalf("rejected checkpoint changed %s: %v", path, err)
		}
	}
}
