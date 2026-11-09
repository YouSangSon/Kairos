package observation

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
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

func (h *ReadHandle) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if h == nil || !authorized(h.statements, query, args) {
		return nil, ErrStatement
	}
	h.session.mu.Lock()
	defer h.session.mu.Unlock()
	if !h.session.active {
		return nil, ErrReference
	}
	return h.session.tx.QueryContext(ctx, query, args...)
}

func (h *ReadHandle) QueryOne(ctx context.Context, query string, args ...any) Scanner {
	if h == nil || !authorized(h.statements, query, args) {
		return failedScan{ErrStatement}
	}
	h.session.mu.Lock()
	defer h.session.mu.Unlock()
	if !h.session.active {
		return failedScan{ErrReference}
	}
	return h.session.tx.QueryRowContext(ctx, query, args...)
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

var sqlTokens = regexp.MustCompile(`'(?:''|[^'])*'|"(?:""|[^"])*"|\[[^\]]*\]|[A-Za-z_][A-Za-z0-9_]*|[?;(),.]`)

func validateStatement(owner string, statement Statement, write bool) error {
	tokens := sqlTokens.FindAllString(statement.SQL, -1)
	if len(tokens) == 0 || statement.Arguments < 0 || strings.Contains(statement.SQL, "--") || strings.Contains(statement.SQL, "/*") {
		return ErrStatement
	}
	operation := strings.ToUpper(tokens[0])
	if write {
		if operation != "INSERT" {
			return ErrStatement
		}
	} else if operation != "SELECT" {
		return ErrStatement
	}
	arguments, tables, catalog := 0, 0, false
	for i, token := range tokens {
		upper := strings.ToUpper(token)
		if token == "?" {
			arguments++
		}
		if token == ";" {
			return ErrStatement
		}
		if strings.HasPrefix(token, "'") {
			continue
		}
		switch upper {
		case "ATTACH", "DETACH", "PRAGMA", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "REPLACE", "LOAD_EXTENSION", "WRITEFILE", "READFILE":
			return ErrStatement
		case "INSERT":
			if !write || i != 0 {
				return ErrStatement
			}
		}
		if upper != "FROM" && upper != "JOIN" && upper != "INTO" {
			continue
		}
		if i+1 >= len(tokens) {
			return ErrStatement
		}
		next := tokens[i+1]
		if next == "(" {
			continue
		}
		if i+2 < len(tokens) && tokens[i+2] == "." {
			return ErrStatement
		}
		table := strings.Trim(next, "\"[]")
		if ownership.IsCatalog(table) {
			catalog = true
			continue
		}
		actual, ok := ownership.Owner(table)
		if !ok || (actual != owner && table != ownership.ControlTable) {
			return fmt.Errorf("%w: %s cannot access %s", ErrStatement, owner, table)
		}
		if write && table != "owner_price_series_versions" {
			return ErrStatement
		}
		tables++
	}
	if arguments != statement.Arguments || (write && (tables != 1 || catalog)) {
		return ErrStatement
	}
	if catalog {
		if !strings.Contains(strings.ToUpper(statement.SQL), "WHERE") {
			return ErrStatement
		}
		if len(statement.Objects) == 0 {
			bounded := false
			for _, table := range ownership.Tables(owner) {
				bounded = bounded || strings.Contains(statement.SQL, "'"+table+"'")
			}
			if !bounded {
				return ErrStatement
			}
		} else if len(statement.ObjectArguments) == 0 {
			return ErrStatement
		}
		for _, object := range statement.Objects {
			owned := false
			for _, table := range ownership.Tables(owner) {
				owned = owned || object == table || strings.HasPrefix(object, table+"_")
			}
			if !owned {
				return ErrStatement
			}
		}
	}
	for _, index := range statement.ObjectArguments {
		if index < 0 || index >= statement.Arguments {
			return ErrStatement
		}
	}
	return nil
}
