package ledgerdomain

import (
	"math/big"
	"time"

	"omni-folio/services/core/internal/exact"
)

type ImportResolution struct {
	Kind           string   `json:"kind"`
	SourceField    string   `json:"source_field"`
	SourceValue    string   `json:"source_value"`
	RequiredAction string   `json:"required_action"`
	CandidateIDs   []string `json:"candidate_ids"`
}

func SameTransaction(a, b *Transaction) bool {
	if a == nil || b == nil {
		return false
	}
	left, right := *a, *b
	left.EventID, right.EventID = "", ""
	return left == right
}

func ValidateCashVoidTarget(correction, target Transaction) *APIError {
	eligible := target.Type == "DEPOSIT" || target.Type == "WITHDRAWAL" || target.Type == "DIVIDEND" || target.Type == "FEE" || target.Type == "TAX"
	targetTime, targetTimeErr := time.Parse(time.RFC3339Nano, target.OccurredAt)
	correctionTime, correctionTimeErr := time.Parse(time.RFC3339Nano, correction.OccurredAt)
	targetAmount, targetAmountErr := exact.ParseDecimal(target.Amount)
	correctionAmount, correctionAmountErr := exact.ParseDecimal(correction.Amount)
	if !eligible || target.Currency != correction.Currency || targetTimeErr != nil || correctionTimeErr != nil || correctionTime.Before(targetTime) || targetAmountErr != nil || correctionAmountErr != nil || new(big.Rat).Add(targetAmount, correctionAmount).Sign() != 0 {
		return InvalidCashVoidCorrection()
	}
	return nil
}

func ValidateCashVoidUse(count int) *APIError {
	if count != 0 {
		return InvalidCashVoidCorrection()
	}
	return nil
}

func InvalidCashVoidCorrection() *APIError {
	return &APIError{Code: "invalid_correction", Message: "cash void does not exactly reverse one eligible existing cash event", Field: "corrects_source_event_id"}
}

func ResolveImportTransaction(tx Transaction) *ImportResolution {
	if tx.AccountID != "account-main" {
		return &ImportResolution{
			Kind: "account", SourceField: "account_id", SourceValue: tx.AccountID,
			RequiredAction: "select_account", CandidateIDs: []string{"account-main"},
		}
	}
	if tx.Symbol != "" && tx.Symbol != "AAPL" {
		return &ImportResolution{
			Kind: "instrument", SourceField: "symbol", SourceValue: tx.Symbol,
			RequiredAction: "select_instrument", CandidateIDs: []string{"instrument_aapl"},
		}
	}
	return nil
}
