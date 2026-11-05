package ledger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"math/big"
	"omni-folio/services/core/internal/application/fault"
	"omni-folio/services/core/internal/exact"
	"omni-folio/services/core/internal/ledgerdomain"
	"time"
	"unicode/utf8"
)

const maxBodyBytes = 1 << 20
const maxImportRows = 10_000
const csvSchema = "omni-folio.csv.v1"
const mappingSchema = "canonical-transaction.v4"

type UseCases struct {
	store Repository
	now   func() time.Time
	id    func(string) string
	ttl   func() time.Duration
}

func New(store Repository, now func() time.Time, id func(string) string, ttl func() time.Duration) *UseCases {
	return &UseCases{store, now, id, ttl}
}
func (s *UseCases) Preview(ctx context.Context, body []byte) (*ImportPreview, *fault.Error) {
	if len(body) == 0 {
		return nil, &fault.Error{Status: 400, Body: APIError{Code: "empty_csv", Message: "CSV body must not be empty"}}
	}
	if len(body) > maxBodyBytes {
		return nil, &fault.Error{Status: 400, Body: APIError{Code: "csv_too_large", Message: "CSV body must not exceed 1048576 bytes"}}
	}
	if !utf8.Valid(body) {
		return nil, &fault.Error{Status: 400, Body: APIError{Code: "invalid_encoding", Message: "CSV body must be UTF-8"}}
	}
	var result *ImportPreview
	err := s.store.Write(ctx, func(scope WriteScope) error {
		var err *fault.Error
		result, err = s.previewInScope(ctx, scope, body)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fault.As(err)
	}
	return result, nil
}
func (s *UseCases) Apply(ctx context.Context, req ApplyRequest) (*ApplyReceipt, *fault.Error) {
	if req.PreviewID == "" || req.IdempotencyKey == "" {
		return nil, &fault.Error{Status: 400, Body: APIError{Code: "required", Message: "preview_id and idempotency_key are required"}}
	}
	received := s.now().UTC()
	var result *ApplyReceipt
	err := s.store.Write(ctx, func(scope WriteScope) error {
		var err *fault.Error
		result, err = s.applyInScope(ctx, scope, req, received)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, fault.As(err)
	}
	return result, nil
}
func (s *UseCases) previewInScope(ctx context.Context, tx WriteScope, body []byte) (*ImportPreview, *fault.Error) {
	rev, err := tx.CurrentRevision(ctx)
	if err != nil {
		return nil, fault.Internal(err)
	}
	rows, parseErr := s.parseCSV(ctx, tx, body)
	if parseErr != nil {
		return nil, parseErr
	}
	totals := PreviewTotals{TotalRows: len(rows)}
	for _, row := range rows {
		switch row.Status {
		case "new":
			totals.NewRows++
		case "duplicate":
			totals.DuplicateRows++
		case "error":
			totals.ErrorRows++
		case "unresolved":
			totals.UnresolvedRows++
		}
	}
	hash := sha256.Sum256(body)
	created := s.now().UTC()
	preview := &ImportPreview{
		PreviewID: s.id("preview"), FileSHA256: hex.EncodeToString(hash[:]), SchemaVersion: csvSchema, MappingVersion: mappingSchema,
		LedgerRevision: ledgerdomain.Revision(rev), CanApply: totals.ErrorRows == 0 && totals.UnresolvedRows == 0,
		Rows: rows, Totals: totals,
	}
	preview.PreviewFingerprint = PreviewFingerprint(preview.FileSHA256, preview.SchemaVersion, preview.MappingVersion, preview.LedgerRevision)
	encoded, err := json.Marshal(preview)
	if err != nil {
		return nil, fault.Internal(err)
	}
	if err := tx.RecordPreview(ctx, StoredPreview{PreviewID: preview.PreviewID, FileSHA256: preview.FileSHA256, LedgerRevision: rev, CanApply: preview.CanApply, JSON: string(encoded), ExpiresAt: created.Add(s.ttl()).Format(time.RFC3339Nano), CreatedAt: created.Format(time.RFC3339Nano)}); err != nil {
		return nil, fault.Internal(err)
	}
	return preview, nil
}

func (s *UseCases) parseCSV(ctx context.Context, q Reader, body []byte) ([]PreviewRow, *fault.Error) {
	r := csv.NewReader(bytes.NewReader(body))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil || len(records) == 0 {
		return nil, &fault.Error{Status: 400, Body: APIError{Code: "invalid_csv", Message: "body must be valid CSV with a header"}}
	}
	if len(records)-1 > maxImportRows {
		return nil, &fault.Error{Status: 400, Body: APIError{Code: "too_many_rows", Message: "CSV must not contain more than 10000 data rows"}}
	}
	required := []string{"source_event_id", "account_id", "type", "occurred_at", "symbol", "quantity", "price", "fee", "currency", "amount"}
	columns := make(map[string]int, len(records[0]))
	for i, name := range records[0] {
		if _, exists := columns[name]; exists {
			return nil, &fault.Error{Status: 400, Body: APIError{Code: "invalid_csv_header", Message: "CSV header names must be unique"}}
		}
		columns[name] = i
	}
	for _, name := range required {
		if _, ok := columns[name]; !ok {
			return nil, &fault.Error{Status: 400, Body: APIError{Code: "invalid_csv_header", Message: "CSV is missing required column " + name, Field: name}}
		}
	}
	value := func(record []string, name string) string {
		i, ok := columns[name]
		if !ok || i >= len(record) {
			return ""
		}
		return record[i]
	}
	seen := map[string]PreviewRow{}
	seenCorrectionTargets := map[string]bool{}
	result := make([]PreviewRow, 0, len(records)-1)
	for i, record := range records[1:] {
		row := PreviewRow{RowNumber: i + 2}
		get := func(name string) string { return value(record, name) }
		tx, fieldErrors := ledgerdomain.Normalize(Transaction{
			EventID: s.id("event"), SourceEventID: get("source_event_id"), AccountID: get("account_id"),
			Type: get("type"), OccurredAt: get("occurred_at"), Symbol: get("symbol"), Quantity: get("quantity"), Price: get("price"), Fee: get("fee"), Currency: get("currency"), Amount: get("amount"), CounterCurrency: get("counter_currency"), CounterAmount: get("counter_amount"), CorrectsSourceEventID: get("corrects_source_event_id"),
		})
		if len(fieldErrors) != 0 {
			row.Status, row.Errors = "error", fieldErrors
			result = append(result, row)
			continue
		}
		if resolution := unresolved(tx); resolution != nil {
			row.Status, row.Transaction, row.Resolution = "unresolved", tx, resolution
			result = append(result, row)
			continue
		}
		key := tx.AccountID + "\x00" + tx.SourceEventID
		if prior, ok := seen[key]; ok {
			if !SameTransaction(prior.Transaction, tx) {
				row.Status, row.Transaction, row.Errors = "error", tx, []APIError{{Code: "source_event_conflict", Message: "source_event_id was already used with different transaction data", Field: "source_event_id"}}
			} else {
				tx.EventID = prior.Transaction.EventID
				row.Status, row.Transaction, row.CorrectionTarget, row.DuplicateOf = "duplicate", tx, prior.CorrectionTarget, tx.EventID
			}
			result = append(result, row)
			continue
		}
		stored, err := q.TransactionBySource(ctx, tx.AccountID, tx.SourceEventID)
		switch {
		case err == nil:
			if !SameTransaction(stored, tx) {
				row.Status, row.Transaction, row.Errors = "error", tx, []APIError{{Code: "source_event_conflict", Message: "source_event_id was already used with different transaction data", Field: "source_event_id"}}
				break
			}
			tx.EventID = stored.EventID
			row.Status, row.Transaction, row.DuplicateOf = "duplicate", tx, stored.EventID
			if tx.Type == "CASH_VOID" {
				target, validationErr, err := ValidateCashVoid(ctx, q, tx, false)
				if err != nil {
					return nil, fault.Internal(err)
				}
				if validationErr != nil {
					return nil, fault.Internal(errors.New("stored cash void failed validation"))
				}
				row.CorrectionTarget = target
			}
		case errors.Is(err, fault.ErrNotFound):
			row.Status, row.Transaction = "new", tx
			if tx.Type == "CASH_VOID" {
				target, validationErr, err := ValidateCashVoid(ctx, q, tx, true)
				if err != nil {
					return nil, fault.Internal(err)
				}
				if validationErr != nil {
					row.Status, row.Errors = "error", []APIError{*validationErr}
				} else {
					targetKey := tx.AccountID + "\x00" + tx.CorrectsSourceEventID
					if seenCorrectionTargets[targetKey] {
						row.Status, row.Errors = "error", []APIError{*invalidCorrection()}
					} else {
						seenCorrectionTargets[targetKey] = true
						row.CorrectionTarget = target
					}
				}
			}
		case err != nil:
			return nil, fault.Internal(err)
		}
		if row.Status != "error" {
			seen[key] = row
		}
		result = append(result, row)
	}
	return result, nil
}

func SameTransaction(a, b *Transaction) bool {
	if a == nil || b == nil {
		return false
	}
	left, right := *a, *b
	left.EventID, right.EventID = "", ""
	return left == right
}

func ValidateCashVoid(ctx context.Context, q Reader, correction *Transaction, requireUnused bool) (*CorrectionTarget, *APIError, error) {
	target, err := q.TransactionBySource(ctx, correction.AccountID, correction.CorrectsSourceEventID)
	if errors.Is(err, fault.ErrNotFound) {
		return nil, invalidCorrection(), nil
	}
	if err != nil {
		return nil, nil, err
	}
	eligible := target.Type == "DEPOSIT" || target.Type == "WITHDRAWAL" || target.Type == "DIVIDEND" || target.Type == "FEE" || target.Type == "TAX"
	targetTime, targetTimeErr := time.Parse(time.RFC3339Nano, target.OccurredAt)
	correctionTime, correctionTimeErr := time.Parse(time.RFC3339Nano, correction.OccurredAt)
	targetAmount, targetAmountErr := exact.ParseDecimal(target.Amount)
	correctionAmount, correctionAmountErr := exact.ParseDecimal(correction.Amount)
	if !eligible || target.Currency != correction.Currency || targetTimeErr != nil || correctionTimeErr != nil || correctionTime.Before(targetTime) || targetAmountErr != nil || correctionAmountErr != nil || new(big.Rat).Add(targetAmount, correctionAmount).Sign() != 0 {
		return nil, invalidCorrection(), nil
	}
	if requireUnused {
		count, err := q.CashVoidCount(ctx, correction.AccountID, correction.CorrectsSourceEventID)
		if err != nil {
			return nil, nil, err
		}
		if count != 0 {
			return nil, invalidCorrection(), nil
		}
	}
	return &CorrectionTarget{SourceEventID: target.SourceEventID, Type: target.Type, Currency: target.Currency, Amount: target.Amount}, nil, nil
}

func invalidCorrection() *APIError {
	return &APIError{Code: "invalid_correction", Message: "cash void does not exactly reverse one eligible existing cash event", Field: "corrects_source_event_id"}
}

func unresolved(tx *Transaction) *Resolution {
	if tx.AccountID != "account-main" {
		return &Resolution{
			Kind: "account", SourceField: "account_id", SourceValue: tx.AccountID,
			RequiredAction: "select_account", CandidateIDs: []string{"account-main"},
		}
	}
	if tx.Symbol != "" && tx.Symbol != "AAPL" {
		return &Resolution{
			Kind: "instrument", SourceField: "symbol", SourceValue: tx.Symbol,
			RequiredAction: "select_instrument", CandidateIDs: []string{"instrument_aapl"},
		}
	}
	return nil
}

func PreviewFingerprint(fileSHA, schemaVersion, mappingVersion, ledgerRevision string) string {
	binding, _ := json.Marshal(map[string]string{
		"file_sha256": fileSHA, "schema_version": schemaVersion,
		"mapping_version": mappingVersion, "ledger_revision": ledgerRevision,
	})
	hash := sha256.Sum256(binding)
	return hex.EncodeToString(hash[:])
}

func (s *UseCases) applyInScope(ctx context.Context, tx WriteScope, req ApplyRequest, received time.Time) (*ApplyReceipt, *fault.Error) {
	requestBytes, _ := json.Marshal(req)
	requestHash := sha256.Sum256(requestBytes)
	requestSHA := hex.EncodeToString(requestHash[:])
	prior, err := tx.Receipt(ctx, req.IdempotencyKey)
	priorHash, priorJSON := prior.RequestSHA256, prior.JSON
	if err == nil {
		if priorHash != requestSHA {
			return nil, &fault.Error{Status: 409, Body: APIError{Code: "idempotency_conflict", Message: "idempotency_key was already used with a different payload"}}
		}
		var receipt ApplyReceipt
		if err := json.Unmarshal([]byte(priorJSON), &receipt); err != nil {
			return nil, fault.Internal(err)
		}
		return &receipt, nil
	}
	if !errors.Is(err, fault.ErrNotFound) {
		return nil, fault.Internal(err)
	}
	stored, err := tx.Preview(ctx, req.PreviewID)
	if err != nil {
		if errors.Is(err, fault.ErrNotFound) {
			return nil, &fault.Error{Status: 400, Body: APIError{Code: "preview_not_found", Message: "preview_id does not exist"}}
		}
		return nil, fault.Internal(err)
	}
	previewJSON, fileSHA, expiresAt, previewRev := stored.JSON, stored.FileSHA256, stored.ExpiresAt, stored.LedgerRevision
	expires, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil || !received.Before(expires) {
		return nil, &fault.Error{Status: 409, Body: APIError{Code: "stale_preview", Message: "preview expired; create a new preview"}}
	}
	if !stored.CanApply {
		return nil, &fault.Error{Status: 400, Body: APIError{Code: "preview_has_errors", Message: "preview contains invalid rows"}}
	}
	currentRev, err := tx.CurrentRevision(ctx)
	if err != nil {
		return nil, fault.Internal(err)
	}
	if currentRev != previewRev {
		return nil, &fault.Error{Status: 409, Body: APIError{Code: "stale_preview", Message: "ledger revision changed; create a new preview"}}
	}
	var preview ImportPreview
	if err := json.Unmarshal([]byte(previewJSON), &preview); err != nil {
		return nil, fault.Internal(err)
	}
	expectedFingerprint := PreviewFingerprint(preview.FileSHA256, preview.SchemaVersion, preview.MappingVersion, preview.LedgerRevision)
	if preview.SchemaVersion != csvSchema || preview.MappingVersion != mappingSchema || preview.PreviewFingerprint != expectedFingerprint ||
		preview.FileSHA256 != fileSHA || preview.LedgerRevision != ledgerdomain.Revision(previewRev) {
		return nil, &fault.Error{Status: 409, Body: APIError{Code: "stale_preview", Message: "preview binding changed; create a new preview"}}
	}
	receiptID := s.id("receipt")
	recorded := s.now().UTC()
	applied := 0
	for _, row := range preview.Rows {
		if row.Status != "new" {
			continue
		}
		e := row.Transaction
		if e == nil {
			return nil, fault.Internal(errors.New("preview new row has no transaction"))
		}
		if err := tx.RecordEvent(ctx, *e, receiptID, recorded.Format(time.RFC3339Nano)); err != nil {
			return nil, fault.Internal(err)
		}
		applied++
	}
	after := currentRev + int64(applied)
	if err := tx.RecordAppliedRevision(ctx, after, recorded.Format(time.RFC3339Nano)); err != nil {
		return nil, fault.Internal(err)
	}
	if _, err := Snapshot(ctx, tx); err != nil {
		log.Printf("ledger invariant validation failed: %v", err)
		return nil, &fault.Error{Status: 400, Body: APIError{Code: "invalid_ledger", Message: "ledger invariant validation failed"}}
	}
	receipt := &ApplyReceipt{
		ReceiptID: receiptID, PreviewID: req.PreviewID, IdempotencyKey: req.IdempotencyKey, FileSHA256: fileSHA,
		LedgerRevisionBefore: ledgerdomain.Revision(currentRev), LedgerRevisionAfter: ledgerdomain.Revision(after), AppliedRows: applied,
		SkippedDuplicateRows: preview.Totals.DuplicateRows, ReceivedAt: received.Format(time.RFC3339Nano), RecordedAt: recorded.Format(time.RFC3339Nano),
	}
	receiptBytes, _ := json.Marshal(receipt)
	if err := tx.RecordReceipt(ctx, StoredReceipt{IdempotencyKey: req.IdempotencyKey, RequestSHA256: requestSHA, ReceiptID: receiptID, JSON: string(receiptBytes)}); err != nil {
		return nil, fault.Internal(err)
	}
	return receipt, nil
}
