package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	ledger "omni-folio/services/core/internal/modules/ledger/internal/application"
	"omni-folio/services/core/internal/storage/observation"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Service struct {
	*ledger.UseCases
	db                *sql.DB
	now               func() time.Time
	id                func(string) string
	activityCursorKey [32]byte
	resolver          *observation.ReadResolver
}
type queryer = Querier
type orderQuerier = Querier
type appError = contracts.Error
type APIError = contracts.APIError
type Transaction = contracts.Transaction
type PortfolioSnapshot = contracts.PortfolioSnapshot
type LedgerActivityPage = contracts.LedgerActivityPage
type LedgerActivity = contracts.LedgerActivity
type ledgerActivityCursor = contracts.ActivityCursor
type InstrumentLedger = contracts.InstrumentLedger
type ledgerReplayPreview = contracts.ReplayPreview
type BrokerPositionDifference = contracts.BrokerPositionDifference
type BrokerKnownGoodSnapshot = contracts.BrokerKnownGoodSnapshot
type BrokerReconciliationView = contracts.BrokerReconciliationView
type KiwoomSnapshot = contracts.BrokerSnapshot
type KiwoomTotals = contracts.BrokerTotals
type KiwoomPosition = contracts.BrokerPosition
type KiwoomOpenOrder = contracts.BrokerOpenOrder
type KiwoomEnvironment = contracts.BrokerEnvironment
type KiwoomExchange = contracts.BrokerExchange

const (
	KiwoomProduction    KiwoomEnvironment = "production"
	KiwoomMock          KiwoomEnvironment = "mock"
	KiwoomKRX           KiwoomExchange    = "KRX"
	fifoCostBasisPolicy                   = contracts.FIFOCostBasisPolicy
	zeroTime                              = "1970-01-01T00:00:00Z"
)

var strategySHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var kiwoomStockPattern = regexp.MustCompile(`^[0-9]{6}$`)
var orderAliasPattern = regexp.MustCompile(`^kiwoom_(account|order|execution)_[A-Za-z0-9_-]{24}$`)

func parseDecimal(v string) (*big.Rat, error)  { return exact.ParseDecimal(v) }
func formatDecimal(v *big.Rat) (string, error) { return exact.FormatDecimal(v) }
func revision(v int64) string                  { return fmt.Sprintf("rev_%010d", v) }
func orderJSONHash(value any) ([]byte, string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}
func orderAlias(raw, kind string) bool {
	return orderAliasPattern.MatchString(raw) && strings.HasPrefix(raw, "kiwoom_"+kind+"_")
}
func safeOrderID(raw string) bool {
	if raw == "" || len(raw) > 128 || !utf8.ValidString(raw) {
		return false
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
func kiwoomValidTime(raw string) bool {
	if !kiwoomStockPattern.MatchString(raw) {
		return false
	}
	_, err := time.Parse("150405", raw)
	return err == nil
}
func kiwoomText(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	if v == "" || !utf8.ValidString(v) || utf8.RuneCountInString(v) > 80 {
		return "", false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return v, true
}
func snapshotFrom(ctx context.Context, q queryer) (*PortfolioSnapshot, error) {
	return ledger.Snapshot(ctx, NewLedgerReader(q))
}
func snapshotFromPolicy(ctx context.Context, q queryer, p string, r int64) (*PortfolioSnapshot, error) {
	return ledger.SnapshotAt(ctx, NewLedgerReader(q), p, r)
}
func requireSchemaContext(ctx context.Context, q queryer) error {
	var first, latest, count int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MIN(version), 0), COALESCE(MAX(version), 0), COUNT(*) FROM schema_migrations`).Scan(&first, &latest, &count); err != nil {
		return fmt.Errorf("database is not migrated; run migrate first: %w", err)
	}
	if first != 1 || latest != 27 || count != 27 {
		return fmt.Errorf("unsupported schema history %d..%d (%d migrations)", first, latest, count)
	}
	return nil
}
