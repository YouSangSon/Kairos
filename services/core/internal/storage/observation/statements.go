package observation

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	storage "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/ownership"
)

type Statement struct {
	SQL             string
	Arguments       int
	Objects         []string
	ObjectArguments []int
}

type ReadResolver struct {
	registry   *Registry
	owner      string
	statements map[string]Statement
}

type ImportResolver struct {
	reader *ReadResolver
	writes map[string]Statement
}
type ReadHandle struct {
	session    *session
	statements map[string]Statement
}
type ImportHandle struct {
	*ReadHandle
	writes map[string]Statement
}

type Scanner interface{ Scan(...any) error }
type failedScan struct{ err error }

func (s failedScan) Scan(...any) error { return s.err }

func (r *Registry) LedgerReader(statements []Statement) (*ReadResolver, error) {
	return r.reader("ledger", statements)
}
func (r *Registry) MarketReader(statements []Statement) (*ReadResolver, error) {
	return r.reader("marketdata", statements)
}
func (r *Registry) WatchlistReader(statements []Statement) (*ReadResolver, error) {
	return r.reader("watchlist", statements)
}

func (r *Registry) reader(owner string, statements []Statement) (*ReadResolver, error) {
	result := &ReadResolver{r, owner, make(map[string]Statement)}
	for _, statement := range statements {
		if err := validateStatement(owner, statement, false); err != nil {
			return nil, err
		}
		statement.Objects = append([]string(nil), statement.Objects...)
		statement.ObjectArguments = append([]int(nil), statement.ObjectArguments...)
		if prior, ok := result.statements[statement.SQL]; ok && prior.Arguments != statement.Arguments {
			return nil, ErrStatement
		}
		result.statements[statement.SQL] = statement
	}
	return result, nil
}

func (r *Registry) OwnerPriceWriter(reads, writes []Statement) (*ImportResolver, error) {
	reader, err := r.MarketReader(reads)
	if err != nil {
		return nil, err
	}
	result := &ImportResolver{reader: reader, writes: make(map[string]Statement)}
	for _, statement := range writes {
		if err := validateStatement("marketdata", statement, true); err != nil {
			return nil, err
		}
		result.writes[statement.SQL] = statement
	}
	return result, nil
}

func (r *ReadResolver) Resolve(ref storage.ObservationRef) (*ReadHandle, error) {
	if r == nil {
		return nil, ErrReference
	}
	s, err := r.registry.find(ref)
	if err != nil {
		return nil, err
	}
	if r.owner == "watchlist" && s.purpose != watchlist && s.purpose != backup {
		return nil, ErrReference
	}
	return &ReadHandle{s, r.statements}, nil
}

func (r *ReadResolver) Cutoff(ref storage.ObservationRef) (time.Time, error) {
	handle, err := r.Resolve(ref)
	if err != nil {
		return time.Time{}, err
	}
	return handle.session.cutoff, nil
}

func (r *ImportResolver) Resolve(ref storage.ObservationRef) (*ImportHandle, error) {
	if r == nil {
		return nil, ErrReference
	}
	handle, err := r.reader.Resolve(ref)
	if err != nil {
		return nil, err
	}
	if handle.session.purpose != ownerPriceImport {
		return nil, ErrReference
	}
	return &ImportHandle{handle, r.writes}, nil
}

func authorized(statements map[string]Statement, query string, args []any) bool {
	statement, ok := statements[query]
	if !ok || len(args) != statement.Arguments {
		return false
	}
	for _, index := range statement.ObjectArguments {
		if index < 0 || index >= len(args) {
			return false
		}
		value, ok := args[index].(string)
		if !ok {
			return false
		}
		found := false
		for _, object := range statement.Objects {
			found = found || value == object
		}
		if !found {
			return false
		}
	}
	return true
}

func (h *ReadHandle) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	if h == nil || !authorized(h.statements, query, args) {
		return nil, ErrStatement
	}
	h.session.mu.Lock()
	defer h.session.mu.Unlock()
	if !h.session.active {
		return nil, ErrReference
	}
	raw, err := h.session.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	rows := &scopedRows{session: h.session, rows: raw}
	h.session.rows[rows] = struct{}{}
	return rows, nil
}

func (h *ReadHandle) QueryOne(ctx context.Context, query string, args ...any) Scanner {
	if h == nil || !authorized(h.statements, query, args) {
		return failedScan{ErrStatement}
	}
	return &scopedScanner{handle: h, ctx: ctx, query: query, args: append([]any(nil), args...)}
}

func (h *ImportHandle) Execute(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if h == nil || !authorized(h.writes, query, args) {
		return nil, ErrStatement
	}
	h.session.mu.Lock()
	defer h.session.mu.Unlock()
	if !h.session.active || h.session.purpose != ownerPriceImport {
		return nil, ErrReference
	}
	return h.session.tx.ExecContext(ctx, query, args...)
}

func validateStatement(owner string, statement Statement, write bool) error {
	fields := strings.Fields(statement.SQL)
	if len(fields) == 0 {
		return ErrStatement
	}
	operation := strings.ToUpper(fields[0])
	if statement.Arguments < 0 || (write && operation != "INSERT") || (!write && operation != "SELECT") {
		return ErrStatement
	}
	if write && !strings.Contains(statement.SQL, "INTO owner_price_series_versions") {
		return ErrStatement
	}
	if err := ownership.ValidateSQL(owner, statement.SQL, statement.Objects); err != nil {
		return fmt.Errorf("%w: %v", ErrStatement, err)
	}
	indexes, err := ownership.CatalogArguments(statement.SQL)
	if err != nil || len(indexes) != len(statement.ObjectArguments) {
		return ErrStatement
	}
	for i, index := range indexes {
		if index != statement.ObjectArguments[i] {
			return ErrStatement
		}
	}
	for _, index := range statement.ObjectArguments {
		if index < 0 || index >= statement.Arguments {
			return ErrStatement
		}
	}
	for _, object := range statement.Objects {
		actual, ok := ownership.ObjectOwner(object)
		if !ok || actual != owner {
			return ErrStatement
		}
	}
	return nil
}
