package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	"omni-folio/services/core/internal/modules/ledger/internal/domain"
	"time"
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
func (s *UseCases) PreviewImport(ctx context.Context, input contracts.ImportInput) (*ImportPreview, *contracts.Error) {
	var result *ImportPreview
	err := s.store.Write(ctx, func(scope WriteScope) error {
		var err *contracts.Error
		result, err = s.previewInScope(ctx, scope, input)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, contracts.AsError(err)
	}
	return result, nil
}
func (s *UseCases) ApplyImport(ctx context.Context, req ApplyRequest) (*ApplyReceipt, *contracts.Error) {
	if req.PreviewID == "" || req.IdempotencyKey == "" {
		return nil, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError{Code: "required", Message: "preview_id and idempotency_key are required"}}
	}
	received := s.now().UTC()
	var result *ApplyReceipt
	err := s.store.Write(ctx, func(scope WriteScope) error {
		var err *contracts.Error
		result, err = s.applyInScope(ctx, scope, req, received)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, contracts.AsError(err)
	}
	return result, nil
}
func (s *UseCases) previewInScope(ctx context.Context, tx WriteScope, input contracts.ImportInput) (*ImportPreview, *contracts.Error) {
	rev, err := tx.CurrentRevision(ctx)
	if err != nil {
		return nil, contracts.InternalError(err)
	}
	rows, parseErr := s.normalizeRows(ctx, tx, input.Rows)
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
	created := s.now().UTC()
	preview := &ImportPreview{
		PreviewID: s.id("preview"), FileSHA256: input.FileSHA256, SchemaVersion: csvSchema, MappingVersion: mappingSchema,
		LedgerRevision: ledgerdomain.Revision(rev), CanApply: ledgerdomain.PreviewCanApply(totals.ErrorRows, totals.UnresolvedRows),
		Rows: rows, Totals: totals,
	}
	preview.PreviewFingerprint = PreviewFingerprint(preview.FileSHA256, preview.SchemaVersion, preview.MappingVersion, preview.LedgerRevision)
	encoded, err := json.Marshal(preview)
	if err != nil {
		return nil, contracts.InternalError(err)
	}
	if err := tx.RecordPreview(ctx, StoredPreview{PreviewID: preview.PreviewID, FileSHA256: preview.FileSHA256, LedgerRevision: rev, CanApply: preview.CanApply, JSON: string(encoded), ExpiresAt: created.Add(s.ttl()).Format(time.RFC3339Nano), CreatedAt: created.Format(time.RFC3339Nano)}); err != nil {
		return nil, contracts.InternalError(err)
	}
	return preview, nil
}

func (s *UseCases) normalizeRows(ctx context.Context, q Reader, records []contracts.ImportRow) ([]PreviewRow, *contracts.Error) {
	seen := map[string]PreviewRow{}
	seenCorrectionTargets := map[string]int{}
	result := make([]PreviewRow, 0, len(records))
	for i, record := range records {
		row := PreviewRow{RowNumber: i + 2}
		get := func(name string) string { return record[name] }
		tx, fieldErrors := normalize(Transaction{
			EventID: s.id("event"), SourceEventID: get("source_event_id"), AccountID: get("account_id"),
			Type: get("type"), OccurredAt: get("occurred_at"), Symbol: get("symbol"), Quantity: get("quantity"), Price: get("price"), Fee: get("fee"), Currency: get("currency"), Amount: get("amount"), CounterCurrency: get("counter_currency"), CounterAmount: get("counter_amount"), CorrectsSourceEventID: get("corrects_source_event_id"),
		})
		if len(fieldErrors) != 0 {
			row.Status, row.Errors = "error", fieldErrors
			result = append(result, row)
			continue
		}
		if resolution := (*Resolution)(ledgerdomain.ResolveImportTransaction(ledgerdomain.Transaction(*tx))); resolution != nil {
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
					return nil, contracts.InternalError(err)
				}
				if validationErr != nil {
					return nil, contracts.InternalError(errors.New("stored cash void failed validation"))
				}
				row.CorrectionTarget = target
			}
		case errors.Is(err, contracts.ErrNotFound):
			row.Status, row.Transaction = "new", tx
			if tx.Type == "CASH_VOID" {
				target, validationErr, err := ValidateCashVoid(ctx, q, tx, true)
				if err != nil {
					return nil, contracts.InternalError(err)
				}
				if validationErr != nil {
					row.Status, row.Errors = "error", []APIError{*validationErr}
				} else {
					targetKey := tx.AccountID + "\x00" + tx.CorrectsSourceEventID
					if rejection := ledgerdomain.ValidateCashVoidUse(seenCorrectionTargets[targetKey]); rejection != nil {
						row.Status, row.Errors = "error", []APIError{APIError(*rejection)}
					} else {
						seenCorrectionTargets[targetKey] = 1
						row.CorrectionTarget = target
					}
				}
			}
		case err != nil:
			return nil, contracts.InternalError(err)
		}
		if row.Status != "error" {
			seen[key] = row
		}
		result = append(result, row)
	}
	return result, nil
}

func SameTransaction(a, b *Transaction) bool {
	return ledgerdomain.SameTransaction((*ledgerdomain.Transaction)(a), (*ledgerdomain.Transaction)(b))
}

func ValidateCashVoid(ctx context.Context, q Reader, correction *Transaction, requireUnused bool) (*CorrectionTarget, *APIError, error) {
	target, err := q.TransactionBySource(ctx, correction.AccountID, correction.CorrectsSourceEventID)
	if errors.Is(err, contracts.ErrNotFound) {
		return nil, invalidCorrection(), nil
	}
	if err != nil {
		return nil, nil, err
	}
	if rejection := ledgerdomain.ValidateCashVoidTarget(ledgerdomain.Transaction(*correction), ledgerdomain.Transaction(*target)); rejection != nil {
		return nil, (*APIError)(rejection), nil
	}
	if requireUnused {
		count, err := q.CashVoidCount(ctx, correction.AccountID, correction.CorrectsSourceEventID)
		if err != nil {
			return nil, nil, err
		}
		if rejection := ledgerdomain.ValidateCashVoidUse(count); rejection != nil {
			return nil, (*APIError)(rejection), nil
		}
	}
	return &CorrectionTarget{SourceEventID: target.SourceEventID, Type: target.Type, Currency: target.Currency, Amount: target.Amount}, nil, nil
}

func invalidCorrection() *APIError {
	return (*APIError)(ledgerdomain.InvalidCashVoidCorrection())
}

func PreviewFingerprint(fileSHA, schemaVersion, mappingVersion, ledgerRevision string) string {
	binding, _ := json.Marshal(map[string]string{
		"file_sha256": fileSHA, "schema_version": schemaVersion,
		"mapping_version": mappingVersion, "ledger_revision": ledgerRevision,
	})
	hash := sha256.Sum256(binding)
	return hex.EncodeToString(hash[:])
}

func (s *UseCases) applyInScope(ctx context.Context, tx WriteScope, req ApplyRequest, received time.Time) (*ApplyReceipt, *contracts.Error) {
	requestBytes, _ := json.Marshal(req)
	requestHash := sha256.Sum256(requestBytes)
	requestSHA := hex.EncodeToString(requestHash[:])
	prior, err := tx.Receipt(ctx, req.IdempotencyKey)
	priorHash, priorJSON := prior.RequestSHA256, prior.JSON
	if err == nil {
		if priorHash != requestSHA {
			return nil, &contracts.Error{Kind: contracts.Conflict, Body: APIError{Code: "idempotency_conflict", Message: "idempotency_key was already used with a different payload"}}
		}
		var receipt ApplyReceipt
		if err := json.Unmarshal([]byte(priorJSON), &receipt); err != nil {
			return nil, contracts.InternalError(err)
		}
		return &receipt, nil
	}
	if !errors.Is(err, contracts.ErrNotFound) {
		return nil, contracts.InternalError(err)
	}
	stored, err := tx.Preview(ctx, req.PreviewID)
	if err != nil {
		if errors.Is(err, contracts.ErrNotFound) {
			return nil, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError{Code: "preview_not_found", Message: "preview_id does not exist"}}
		}
		return nil, contracts.InternalError(err)
	}
	previewJSON, fileSHA, expiresAt, previewRev := stored.JSON, stored.FileSHA256, stored.ExpiresAt, stored.LedgerRevision
	if rejection := ledgerdomain.ValidatePreviewLifetime(received, expiresAt); rejection != nil {
		return nil, &contracts.Error{Kind: contracts.Conflict, Body: APIError(*rejection)}
	}
	if rejection := ledgerdomain.ValidateApplicablePreview(stored.CanApply); rejection != nil {
		return nil, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError(*rejection)}
	}
	currentRev, err := tx.CurrentRevision(ctx)
	if err != nil {
		return nil, contracts.InternalError(err)
	}
	if rejection := ledgerdomain.ValidatePreviewRevision(currentRev, previewRev); rejection != nil {
		return nil, &contracts.Error{Kind: contracts.Conflict, Body: APIError(*rejection)}
	}
	var preview ImportPreview
	if err := json.Unmarshal([]byte(previewJSON), &preview); err != nil {
		return nil, contracts.InternalError(err)
	}
	expectedFingerprint := PreviewFingerprint(preview.FileSHA256, preview.SchemaVersion, preview.MappingVersion, preview.LedgerRevision)
	if preview.SchemaVersion != csvSchema || preview.MappingVersion != mappingSchema || preview.PreviewFingerprint != expectedFingerprint ||
		preview.FileSHA256 != fileSHA || preview.LedgerRevision != ledgerdomain.Revision(previewRev) {
		return nil, &contracts.Error{Kind: contracts.Conflict, Body: APIError{Code: "stale_preview", Message: "preview binding changed; create a new preview"}}
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
			return nil, contracts.InternalError(errors.New("preview new row has no transaction"))
		}
		if err := tx.RecordEvent(ctx, *e, receiptID, recorded.Format(time.RFC3339Nano)); err != nil {
			return nil, contracts.InternalError(err)
		}
		applied++
	}
	after := currentRev + int64(applied)
	if err := tx.RecordAppliedRevision(ctx, after, recorded.Format(time.RFC3339Nano)); err != nil {
		return nil, contracts.InternalError(err)
	}
	if _, err := Snapshot(ctx, tx); err != nil {
		return nil, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError{Code: "invalid_ledger", Message: "ledger invariant validation failed"}}
	}
	receipt := &ApplyReceipt{
		ReceiptID: receiptID, PreviewID: req.PreviewID, IdempotencyKey: req.IdempotencyKey, FileSHA256: fileSHA,
		LedgerRevisionBefore: ledgerdomain.Revision(currentRev), LedgerRevisionAfter: ledgerdomain.Revision(after), AppliedRows: applied,
		SkippedDuplicateRows: preview.Totals.DuplicateRows, ReceivedAt: received.Format(time.RFC3339Nano), RecordedAt: recorded.Format(time.RFC3339Nano),
	}
	receiptBytes, _ := json.Marshal(receipt)
	if err := tx.RecordReceipt(ctx, StoredReceipt{IdempotencyKey: req.IdempotencyKey, RequestSHA256: requestSHA, ReceiptID: receiptID, JSON: string(receiptBytes)}); err != nil {
		return nil, contracts.InternalError(err)
	}
	return receipt, nil
}
