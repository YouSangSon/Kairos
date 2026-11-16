package files

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	privatefile "omni-folio/services/core/internal/files/private"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
	"path/filepath"
	"regexp"
	"strconv"
)

type LedgerActivity = c.LedgerActivity
type ExportPublisher struct{}

const maxLedgerExportBytes = 32 << 20

var ledgerExportSymbolPattern = regexp.MustCompile(`^[\pL\pN][\pL\pN._/-]*$`)

func ledgerExportTarget(dbPath, out string) (string, string, error) {
	database, err := filepath.EvalSymlinks(dbPath)
	if err != nil {
		return "", "", err
	}
	database, err = filepath.Abs(database)
	if err != nil {
		return "", "", err
	}
	parent, target, err := privatefile.Target(out)
	if err != nil {
		return "", "", err
	}
	input, err := filepath.Abs(dbPath)
	if err != nil {
		return "", "", err
	}
	inputParent, err := filepath.EvalSymlinks(filepath.Dir(input))
	if err != nil {
		return "", "", err
	}
	for _, name := range []string{database, filepath.Join(inputParent, filepath.Base(input))} {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			if target == name+suffix {
				return "", "", errors.New("ledger export cannot target database files")
			}
		}
	}
	return parent, target, nil
}

func (ExportPublisher) Publish(ctx context.Context, dbPath, out string, events []LedgerActivity) (string, error) {
	parent, target, err := ledgerExportTarget(dbPath, out)
	if err != nil {
		return "", err
	}
	return privatefile.Publish(ctx, parent, target, ".omni-ledger-export-*", func(output io.Writer) error {
		limited := &ledgerExportLimitWriter{writer: output, remaining: maxLedgerExportBytes}
		return writeLedgerExportCSV(ctx, limited, events)
	})
}

func writeLedgerExportCSV(ctx context.Context, output io.Writer, events []LedgerActivity) error {
	w := csv.NewWriter(output)
	if err := w.Write([]string{"type", "occurred_at", "recorded_at", "symbol", "quantity", "price", "fee", "currency", "amount", "counter_currency", "counter_amount", "is_correction"}); err != nil {
		return err
	}
	text := func(value *string) string {
		if value == nil {
			return ""
		}
		return *value
	}
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		symbol := text(event.Symbol)
		if symbol != "" && !ledgerExportSymbolPattern.MatchString(symbol) {
			return errors.New("ledger export symbol is unsafe for CSV")
		}
		if err := w.Write([]string{event.Type, event.OccurredAt, event.RecordedAt, symbol, text(event.Quantity), text(event.Price), text(event.Fee), event.Currency, event.Amount, text(event.CounterCurrency), text(event.CounterAmount), strconv.FormatBool(event.IsCorrection)}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

type ledgerExportLimitWriter struct {
	writer    io.Writer
	remaining int
}

func (w *ledgerExportLimitWriter) Write(raw []byte) (int, error) {
	if len(raw) > w.remaining {
		return 0, errors.New("ledger CSV exceeds the export size limit")
	}
	n, err := w.writer.Write(raw)
	w.remaining -= n
	if n != len(raw) && err == nil {
		err = io.ErrShortWrite
	}
	return n, err
}
