package ledgerdomain

import (
	"errors"
	"math/big"
	"omni-folio/services/core/internal/exact"
	"strings"
	"time"
)

func Normalize(input Transaction) (*Transaction, []APIError) {
	tx := &input
	tx.InstrumentID = ""
	var errs []APIError
	for _, field := range []struct{ name, value string }{{"source_event_id", tx.SourceEventID}, {"account_id", tx.AccountID}} {
		if field.value == "" {
			errs = append(errs, APIError{"required", field.name + " is required", field.name})
		}
	}
	if occurredAt, err := time.Parse(time.RFC3339, tx.OccurredAt); err != nil {
		errs = append(errs, APIError{"invalid_datetime", "occurred_at must be an RFC 3339 timestamp", "occurred_at"})
	} else {
		tx.OccurredAt = occurredAt.UTC().Format(time.RFC3339Nano)
	}
	if len(tx.Currency) != 3 || tx.Currency != strings.ToUpper(tx.Currency) || strings.IndexFunc(tx.Currency, func(r rune) bool { return r < 'A' || r > 'Z' }) >= 0 {
		errs = append(errs, APIError{"invalid_currency", "currency must be a three-letter uppercase code", "currency"})
	}
	amount, err := exact.ParseDecimal(tx.Amount)
	if err != nil {
		errs = append(errs, APIError{"invalid_decimal", "amount must be a canonical decimal", "amount"})
	}
	if tx.Type != "CASH_VOID" && tx.CorrectsSourceEventID != "" {
		errs = append(errs, APIError{"invalid_fields", "corrects_source_event_id is only valid for CASH_VOID", "corrects_source_event_id"})
	}
	if tx.Type != "FX_EXCHANGE" && (tx.CounterCurrency != "" || tx.CounterAmount != "") {
		errs = append(errs, APIError{"invalid_fields", "counter currency and amount are only valid for FX_EXCHANGE", "counter_currency"})
	}
	switch tx.Type {
	case "DEPOSIT", "WITHDRAWAL", "FEE", "TAX":
		if tx.Symbol != "" || tx.Quantity != "" || tx.Price != "" || tx.Fee != "" {
			errs = append(errs, APIError{"invalid_fields", tx.Type + " trade fields must be empty", "symbol"})
		}
		tx.Symbol, tx.Quantity, tx.Price, tx.Fee = "", "", "", ""
		if err == nil && ((tx.Type == "DEPOSIT" && amount.Sign() <= 0) || (tx.Type != "DEPOSIT" && amount.Sign() >= 0)) {
			direction := "negative"
			if tx.Type == "DEPOSIT" {
				direction = "positive"
			}
			errs = append(errs, APIError{"invalid_amount", tx.Type + " amount must be " + direction, "amount"})
		}
	case "DIVIDEND":
		if tx.Symbol == "" {
			errs = append(errs, APIError{"required", "symbol is required for DIVIDEND", "symbol"})
		} else {
			tx.InstrumentID = InstrumentIDForSymbol(tx.Symbol)
		}
		if tx.Quantity != "" || tx.Price != "" || tx.Fee != "" {
			errs = append(errs, APIError{"invalid_fields", "DIVIDEND quantity, price, and fee must be empty", "quantity"})
		}
		tx.Quantity, tx.Price, tx.Fee = "", "", ""
		if err == nil && amount.Sign() <= 0 {
			errs = append(errs, APIError{"invalid_amount", "DIVIDEND amount must be positive", "amount"})
		}
	case "SPLIT":
		if tx.Symbol == "" {
			errs = append(errs, APIError{"required", "symbol is required for SPLIT", "symbol"})
		} else {
			tx.InstrumentID = InstrumentIDForSymbol(tx.Symbol)
		}
		positiveDecimal(tx.Quantity, "quantity", &errs)
		if tx.Price != "" || tx.Fee != "" {
			errs = append(errs, APIError{"invalid_fields", "SPLIT price and fee must be empty", "price"})
		}
		tx.Price, tx.Fee = "", ""
		if err == nil && amount.Sign() != 0 {
			errs = append(errs, APIError{"invalid_amount", "SPLIT amount must be zero", "amount"})
		}
	case "CASH_VOID":
		if tx.Symbol != "" || tx.Quantity != "" || tx.Price != "" || tx.Fee != "" {
			errs = append(errs, APIError{"invalid_fields", "CASH_VOID trade fields must be empty", "symbol"})
		}
		tx.Symbol, tx.Quantity, tx.Price, tx.Fee = "", "", "", ""
		if tx.CorrectsSourceEventID == "" {
			errs = append(errs, APIError{"required", "corrects_source_event_id is required for CASH_VOID", "corrects_source_event_id"})
		}
		if err == nil && amount.Sign() == 0 {
			errs = append(errs, APIError{"invalid_amount", "CASH_VOID amount must be non-zero", "amount"})
		}
	case "FX_EXCHANGE":
		if tx.Symbol != "" || tx.Quantity != "" || tx.Price != "" || tx.Fee != "" {
			errs = append(errs, APIError{"invalid_fields", "FX_EXCHANGE trade fields must be empty", "symbol"})
		}
		tx.Symbol, tx.Quantity, tx.Price, tx.Fee = "", "", "", ""
		if err == nil && amount.Sign() >= 0 {
			errs = append(errs, APIError{"invalid_amount", "FX_EXCHANGE amount must be negative", "amount"})
		}
		if tx.CounterCurrency == "" {
			errs = append(errs, APIError{"required", "counter_currency is required for FX_EXCHANGE", "counter_currency"})
		} else if len(tx.CounterCurrency) != 3 || tx.CounterCurrency != strings.ToUpper(tx.CounterCurrency) || strings.IndexFunc(tx.CounterCurrency, func(r rune) bool { return r < 'A' || r > 'Z' }) >= 0 {
			errs = append(errs, APIError{"invalid_currency", "counter_currency must be a three-letter uppercase code", "counter_currency"})
		} else if tx.CounterCurrency == tx.Currency {
			errs = append(errs, APIError{"invalid_currency", "FX_EXCHANGE currencies must be different", "counter_currency"})
		}
		if tx.CounterAmount == "" {
			errs = append(errs, APIError{"required", "counter_amount is required for FX_EXCHANGE", "counter_amount"})
		} else if counterAmount, counterErr := exact.ParseDecimal(tx.CounterAmount); counterErr != nil {
			errs = append(errs, APIError{"invalid_decimal", "counter_amount must be a canonical decimal", "counter_amount"})
		} else if counterAmount.Sign() <= 0 {
			errs = append(errs, APIError{"invalid_amount", "FX_EXCHANGE counter_amount must be positive", "counter_amount"})
		}
	case "BUY", "SELL":
		if tx.Symbol == "" {
			errs = append(errs, APIError{"required", "symbol is required for trades", "symbol"})
		} else {
			tx.InstrumentID = InstrumentIDForSymbol(tx.Symbol)
		}
		quantity, quantityErr := positiveDecimal(tx.Quantity, "quantity", &errs)
		price, priceErr := positiveDecimal(tx.Price, "price", &errs)
		fee, feeErr := nonNegativeDecimal(tx.Fee, "fee", &errs)
		if err == nil && quantityErr == nil && priceErr == nil && feeErr == nil {
			expected := new(big.Rat).Mul(quantity, price)
			expected.Add(expected, fee)
			if tx.Type == "BUY" {
				expected.Neg(expected)
			} else {
				expected.Sub(new(big.Rat).Mul(quantity, price), fee)
			}
			if expected.Cmp(amount) != 0 {
				errs = append(errs, APIError{"amount_mismatch", "amount must equal the signed trade cash impact including fee", "amount"})
			}
		}
	default:
		errs = append(errs, APIError{"invalid_type", "unsupported transaction type", "type"})
	}
	return tx, errs
}

func InstrumentIDForSymbol(symbol string) string {
	return "instrument_" + strings.ToLower(symbol)
}

func positiveDecimal(raw, field string, errs *[]APIError) (*big.Rat, error) {
	v, err := exact.ParseDecimal(raw)
	if err != nil {
		*errs = append(*errs, APIError{"invalid_decimal", field + " must be a canonical decimal", field})
	} else if v.Sign() <= 0 {
		err = errors.New("not positive")
		*errs = append(*errs, APIError{"invalid_decimal", field + " must be positive", field})
	}
	return v, err
}

func nonNegativeDecimal(raw, field string, errs *[]APIError) (*big.Rat, error) {
	v, err := exact.ParseDecimal(raw)
	if err != nil {
		*errs = append(*errs, APIError{"invalid_decimal", field + " must be a canonical decimal", field})
	} else if v.Sign() < 0 {
		err = errors.New("negative")
		*errs = append(*errs, APIError{"invalid_decimal", field + " must be non-negative", field})
	}
	return v, err
}
