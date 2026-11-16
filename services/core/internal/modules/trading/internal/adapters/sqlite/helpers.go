package sqlite

import (
	"context"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"omni-folio/services/core/internal/modules/trading/internal/domain"

	"errors"
	"fmt"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"omni-folio/services/core/migrations"
	"regexp"

	"strings"
	"time"
)

func canonicalDecimal(raw string, nonNegative bool) bool {
	if len(raw) == 0 || len(raw) > 64 {
		return false
	}
	value, err := parseDecimal(raw)
	if err != nil || (nonNegative && value.Sign() < 0) {
		return false
	}
	canonical, err := formatDecimal(value)
	return err == nil && canonical == raw
}

func canonicalUTCString(raw string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	return err == nil && parsed.UTC().Format(time.RFC3339Nano) == raw
}

func migrationTriggerDefinition(script, name string) (string, error) {
	prefix := "CREATE TRIGGER " + name
	start := strings.Index(script, prefix)
	if start < 0 {
		return "", errors.New("trigger is missing")
	}
	rest := script[start:]
	if next := strings.Index(rest[len(prefix):], "\nCREATE TRIGGER "); next >= 0 {
		rest = rest[:len(prefix)+next]
	}
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(rest), ";")), " ")), nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

var validateMarketDataValues = domain.ValidateBarValues

var researchReportDecimal = application.ResearchReportDecimal

var researchReportInteger = application.ResearchReportInteger

const (
	latestSchema                    = 27
	maxBodyBytes                    = 1 << 20
	maxMarketDataBytes              = 1 << 20
	maxMarketDataRows               = 500
	marketDataAdjustmentUnspecified = "unspecified"
)

var (
	migrationFiles     = migrations.Files
	kiwoomStockPattern = regexp.MustCompile(`^[0-9]{6}$`)
)

func parseDecimal(raw string) (*big.Rat, error) { return exact.ParseDecimal(raw) }

func formatDecimal(raw *big.Rat) (string, error) { return exact.FormatDecimal(raw) }

func requireSchemaContext(ctx context.Context, q orderQuerier) error {
	return requireSchemaVersionContext(ctx, q, latestSchema)
}
func requireSchemaVersionContext(ctx context.Context, q orderQuerier, expected int) error {
	var first, latest, count int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MIN(version), 0), COALESCE(MAX(version), 0), COUNT(*) FROM schema_migrations`).Scan(&first, &latest, &count); err != nil {
		return fmt.Errorf("database is not migrated; run migrate first: %w", err)
	}
	if first != 1 || latest != expected || count != expected {
		return fmt.Errorf("unsupported schema history %d..%d (%d migrations)", first, latest, count)
	}
	return nil
}
