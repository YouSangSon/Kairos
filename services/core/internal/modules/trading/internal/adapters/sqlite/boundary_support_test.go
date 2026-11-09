package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"omni-folio/services/core/internal/modules/trading/construction"
	"omni-folio/services/core/internal/modules/trading/contracts"
	tradingcli "omni-folio/services/core/internal/modules/trading/internal/adapters/cli"
	tradinghttp "omni-folio/services/core/internal/modules/trading/internal/adapters/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func projectPath(path string) string {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		panic("test source path unavailable")
	}
	return filepath.Join(filepath.Dir(source), "../../../../../../../..", path)
}
func (s *Store) routes() http.Handler {
	mux := http.NewServeMux()
	tradinghttp.Register(mux, s)
	return mux
}

type boundarySession struct {
	store *Store
	close func() error
}

func (s boundarySession) Trading() contracts.Trading { return s.store }
func (s boundarySession) Close() error               { return s.close() }
func (s boundarySession) RequireSchema() error {
	var first, last, count int
	if err := s.store.db.QueryRow(`SELECT COALESCE(MIN(version),0),COALESCE(MAX(version),0),COUNT(*) FROM schema_migrations`).Scan(&first, &last, &count); err != nil {
		return fmt.Errorf("database is not migrated; run migrate first: %w", err)
	}
	if first != 1 || last != latestSchema || count != latestSchema {
		return fmt.Errorf("unsupported schema history %d..%d (%d migrations)", first, last, count)
	}
	return nil
}

type boundaryResources struct{ store *Store }

func (r boundaryResources) session(db *sql.DB, err error) (construction.Session, error) {
	if err != nil {
		return nil, err
	}
	return boundarySession{newService(db, time.Now, randomID), db.Close}, nil
}
func (r boundaryResources) OpenExisting(path string) (construction.Session, error) {
	if r.store != nil {
		return boundarySession{r.store, func() error { return nil }}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("database must already exist; run migrate first: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("database path must be a regular file")
	}
	return r.session(openDB(path))
}
func (r boundaryResources) OpenReadOnly(ctx context.Context, path string) (construction.Session, error) {
	return r.session(openReadOnlyExistingDB(ctx, path))
}
func (r boundaryResources) OpenReadWrite(ctx context.Context, path string) (construction.Session, error) {
	return r.session(openExistingFileDB(ctx, path, false))
}
func openReadOnlyExistingDB(ctx context.Context, path string) (*sql.DB, error) {
	return openExistingFileDB(ctx, path, true)
}
func openExistingFileDB(ctx context.Context, path string, readOnly bool) (*sql.DB, error) {
	mode, lock, description := "ro", "deferred", "read-only"
	if !readOnly {
		mode, lock, description = "rw", "immediate", "write"
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New(description + " command requires an existing regular database")
	}
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"mode": {mode}, "_txlock": {lock}, "_busy_timeout": {"5000"}, "_foreign_keys": {"on"}}.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func runContext(ctx context.Context, args []string) error {
	return tradingcli.Run(ctx, args[0], args[1:], boundaryResources{}, os.Stdin, os.Stdout)
}
func runLocalPaperCommand(ctx context.Context, args []string, out io.Writer) error {
	return tradingcli.Run(ctx, args[0], args[1:], boundaryResources{}, nil, out)
}
func runPaperPromotionCommand(ctx context.Context, command string, args []string, out io.Writer) error {
	return tradingcli.Run(ctx, command, args, boundaryResources{}, nil, out)
}
func runPaperImprovementCommand(ctx context.Context, args []string, out io.Writer) error {
	return tradingcli.Run(ctx, "paper-improve", args, boundaryResources{}, nil, out)
}
func runPaperInputStatusCommand(ctx context.Context, args []string, out io.Writer) error {
	return tradingcli.Run(ctx, "paper-input-status", args, boundaryResources{}, nil, out)
}
func (s *Store) runLocalPaperStream(ctx context.Context, account, selection string, input *os.File) error {
	return tradingcli.Run(ctx, "paper-execute-stream", []string{"-account", account, "-expected-current-event", selection, "-arm-paper"}, boundaryResources{store: s}, input, io.Discard)
}

type ledgerExportFailWriter struct{ err error }

func (w ledgerExportFailWriter) Write([]byte) (int, error) { return 0, w.err }
func paperSnapshotRow(day, symbol, close string) string {
	return fmt.Sprintf("%sT06:30:00Z,%s,KRX,Asia/Seoul,1d,1000,1010,990,%s,10000,%sT00:00:00Z,%sT06:31:00Z,%sT06:32:00Z", day, symbol, close, day, day, day)
}

func run(args []string) error { return runContext(context.Background(), args) }
