package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"omni-folio/services/core/internal/modules/marketdata/internal/application"
	"omni-folio/services/core/internal/providers/kiwoom"
)

const (
	CandleCheckpointFile          = "candle-checkpoint.sqlite"
	candleCheckpointStagePrefix   = "." + CandleCheckpointFile + ".init-"
	candleCheckpointApplicationID = 1262699344 // KCCP in the SQLite header
	maxCandleCheckpointPages      = 32
	maxCandleCheckpointPayload    = 1 << 20
)

const candleCheckpointSchema = `CREATE TABLE candle_capture_pages (
	page_number INTEGER PRIMARY KEY CHECK (page_number BETWEEN 1 AND 32),
	symbol TEXT NOT NULL CHECK (length(symbol) = 6 AND symbol NOT GLOB '*[^0-9]*'),
	interval TEXT NOT NULL CHECK (length(interval) BETWEEN 2 AND 3),
	started_at TEXT NOT NULL CHECK (length(started_at) BETWEEN 20 AND 30),
	digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
	complete INTEGER NOT NULL CHECK (complete IN (0, 1)),
	payload BLOB NOT NULL CHECK (length(payload) BETWEEN 1 AND 1048576),
	page_sha256 TEXT NOT NULL CHECK (length(page_sha256) = 64 AND page_sha256 NOT GLOB '*[^0-9a-f]*')
) STRICT`

var candleCheckpointSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

type CandleCheckpointPage = application.CandleCheckpointPage

type CandleCheckpoint struct {
	mu       sync.Mutex
	db       *sql.DB
	work     *os.File
	path     string
	pending  string
	symbol   string
	interval string
}

func OpenCandleCheckpoint(ctx context.Context, dir, symbol, interval string) (_ *CandleCheckpoint, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := kiwoom.ValidateMockCandleRequest(symbol, interval); err != nil {
		return nil, err
	}
	work, err := lockCandleCheckpointDirectory(dir)
	if err != nil {
		return nil, err
	}
	var db *sql.DB
	defer func() {
		if resultErr != nil {
			if db != nil {
				resultErr = errors.Join(resultErr, db.Close())
			}
			resultErr = errors.Join(resultErr, work.Close())
		}
	}()

	path := filepath.Join(work.Name(), CandleCheckpointFile)
	info, err := os.Lstat(path)
	var links uint64
	var pending string
	var recovering bool
	switch {
	case err == nil:
		links = candleCheckpointFileLinks(info)
		if links == 0 || links > 2 {
			return nil, errors.New("unsafe candle checkpoint file")
		}
		pending = candleCheckpointMarker(path, info)
		recovering, err = existingCandleCheckpointMarker(pending)
		if err != nil {
			return nil, err
		}
		if recovering && links != 1 {
			return nil, errors.New("unsafe linked candle checkpoint database")
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	if err := checkCandleCheckpointSidecars(path, recovering); err != nil {
		return nil, err
	}
	if links == 0 {
		if err := initializeCandleCheckpoint(ctx, work, path); err != nil {
			return nil, err
		}
		info, err = os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if candleCheckpointFileLinks(info) != 1 {
			return nil, errors.New("unsafe candle checkpoint file")
		}
		pending = candleCheckpointMarker(path, info)
	} else if err := identifyCandleCheckpoint(ctx, work, path, info, links); err != nil {
		return nil, err
	}

	db, err = openCandleCheckpointSQLite(path, false)
	if err != nil {
		return nil, err
	}
	if err := checkCandleCheckpointSchema(ctx, db); err != nil {
		return nil, err
	}
	checkpoint := &CandleCheckpoint{db: db, work: work, path: path, pending: pending, symbol: symbol, interval: interval}
	if _, err := checkpoint.Pages(ctx); err != nil {
		return nil, err
	}
	if recovering {
		// The read above lets SQLite roll back a hot journal before any cold residue is removed.
		if err := db.Close(); err != nil {
			return nil, err
		}
		if err := checkpoint.finishWrite(true); err != nil {
			return nil, err
		}
		db, err = openCandleCheckpointSQLite(path, false)
		if err != nil {
			return nil, err
		}
		checkpoint.db = db
	}
	return checkpoint, nil
}

func (c *CandleCheckpoint) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db == nil {
		return nil
	}
	err := errors.Join(c.db.Close(), c.work.Close())
	c.db = nil
	return err
}

func (c *CandleCheckpoint) Pages(ctx context.Context) ([]CandleCheckpointPage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db == nil {
		return nil, errors.New("candle checkpoint is closed")
	}
	return c.readPages(ctx, c.db)
}

func (c *CandleCheckpoint) Append(ctx context.Context, page CandleCheckpointPage) (resultErr error) {
	startedAt := page.StartedAt.UTC().Format(time.RFC3339Nano)
	if page.Number < 1 || page.Number > maxCandleCheckpointPages || page.StartedAt.IsZero() || !canonicalUTCString(startedAt) ||
		!candleCheckpointSHA.MatchString(page.Digest) || len(page.Payload) == 0 || len(page.Payload) > maxCandleCheckpointPayload {
		return errors.New("invalid candle checkpoint page")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db == nil {
		return errors.New("candle checkpoint is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.beginWrite(); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, c.finishWrite(false)) }()

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	saved, err := c.readPages(ctx, tx)
	if err != nil {
		return err
	}
	if page.Number != len(saved)+1 {
		return errors.New("candle checkpoint page is out of sequence")
	}
	if len(saved) > 0 {
		last := saved[len(saved)-1]
		if last.Complete || !last.StartedAt.Equal(page.StartedAt) {
			return errors.New("candle checkpoint cannot advance")
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO candle_capture_pages
		(page_number, symbol, interval, started_at, digest, complete, payload, page_sha256)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		page.Number, c.symbol, c.interval, startedAt, page.Digest, page.Complete, page.Payload, c.pageChecksum(page, startedAt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *CandleCheckpoint) readPages(ctx context.Context, reader rawQueryer) ([]CandleCheckpointPage, error) {
	rows, err := reader.QueryContext(ctx, `SELECT page_number, symbol, interval, started_at, digest, complete,
		CASE WHEN page_number = (SELECT max(page_number) FROM candle_capture_pages)
			AND length(payload) BETWEEN 1 AND 1048576 THEN payload END,
		page_sha256
		FROM candle_capture_pages ORDER BY page_number LIMIT 33`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pages []CandleCheckpointPage
	var originalStart, lastHash string
	for rows.Next() {
		var page CandleCheckpointPage
		var symbol, interval, startedAt string
		if err := rows.Scan(&page.Number, &symbol, &interval, &startedAt, &page.Digest, &page.Complete, &page.Payload, &lastHash); err != nil {
			return nil, err
		}
		if len(pages) == 0 {
			originalStart = startedAt
		} else if pages[len(pages)-1].Complete {
			return nil, errors.New("completed candle checkpoint has further pages")
		}
		if page.Number != len(pages)+1 || page.Number > maxCandleCheckpointPages || symbol != c.symbol ||
			interval != c.interval || startedAt != originalStart || !candleCheckpointSHA.MatchString(page.Digest) {
			return nil, errors.New("invalid candle checkpoint sequence or scope")
		}
		when, err := time.Parse(time.RFC3339Nano, startedAt)
		if err != nil || when.IsZero() || when.UTC().Format(time.RFC3339Nano) != startedAt {
			return nil, errors.New("invalid candle checkpoint start")
		}
		page.StartedAt = when
		pages = append(pages, page)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pages) > 0 {
		latest := pages[len(pages)-1]
		if len(latest.Payload) == 0 || c.pageChecksum(latest, originalStart) != lastHash {
			return nil, errors.New("invalid candle checkpoint page checksum")
		}
	}
	return pages, nil
}

func (c *CandleCheckpoint) pageChecksum(page CandleCheckpointPage, startedAt string) string {
	hash := sha256.New()
	// Validated metadata cannot contain NUL, and the opaque payload is the final field.
	fmt.Fprintf(hash, "%d\x00%s\x00%s\x00%s\x00%s\x00%t\x00",
		page.Number, c.symbol, c.interval, startedAt, page.Digest, page.Complete)
	hash.Write(page.Payload)
	return hex.EncodeToString(hash.Sum(nil))
}

func initializeCandleCheckpoint(ctx context.Context, work *os.File, path string) (resultErr error) {
	stage, err := os.CreateTemp(work.Name(), candleCheckpointStagePrefix)
	if err != nil {
		return err
	}
	name := stage.Name()
	defer func() {
		if name != "" {
			resultErr = errors.Join(resultErr, os.Remove(name))
		}
	}()
	if err := errors.Join(stage.Chmod(0o600), stage.Close()); err != nil {
		return err
	}
	db, err := openCandleCheckpointSQLite(name, false)
	if err != nil {
		return err
	}
	if err := errors.Join(createCandleCheckpointSchema(ctx, db), db.Close()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Publish only after the schema has committed. The link also prevents replacing a collision.
	if err := os.Link(name, path); err != nil {
		return err
	}
	if err := os.Remove(name); err != nil {
		return err
	}
	name = ""
	return work.Sync()
}

func createCandleCheckpointSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, candleCheckpointSchema); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA application_id = 1262699344`); err != nil {
		return err
	}
	return tx.Commit()
}

func identifyCandleCheckpoint(ctx context.Context, work *os.File, path string, info os.FileInfo, links uint64) (resultErr error) {
	if err := checkCandleCheckpointHeader(path, info); err != nil {
		return err
	}
	// Read the immutable schema without triggering rollback of a foreign journal.
	db, err := openCandleCheckpointSQLite(path, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	if err := checkCandleCheckpointSchema(ctx, db); err != nil {
		return err
	}
	if links == 1 {
		return nil
	}
	var hasPage bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM candle_capture_pages)`).Scan(&hasPage); err != nil {
		return err
	}
	if hasPage {
		return errors.New("unsafe linked candle checkpoint database")
	}
	return recoverCandleCheckpointStage(work, info)
}

func checkCandleCheckpointSchema(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT name, type, sql FROM main.sqlite_schema ORDER BY name LIMIT 2`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return errors.Join(errors.New("unrecognized candle checkpoint schema"), rows.Err())
	}
	var name, kind, ddl string
	if err := rows.Scan(&name, &kind, &ddl); err != nil {
		return err
	}
	if name != "candle_capture_pages" || kind != "table" || ddl != candleCheckpointSchema || rows.Next() {
		return errors.New("unrecognized candle checkpoint schema")
	}
	return rows.Err()
}

func openCandleCheckpointSQLite(path string, immutable bool) (*sql.DB, error) {
	dsn := url.URL{Scheme: "file", Path: path}
	if immutable {
		dsn.RawQuery = "mode=ro&immutable=1"
	} else {
		dsn.RawQuery = "mode=rw&_busy_timeout=0&_journal_mode=DELETE&_synchronous=FULL&_txlock=immediate"
	}
	db, err := sql.Open("sqlite3", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}
