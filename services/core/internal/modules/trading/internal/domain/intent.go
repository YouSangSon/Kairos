package domain

import (
	"errors"
	"omni-folio/services/core/internal/exact"
	"omni-folio/services/core/internal/paperdomain"
	"regexp"
	"time"
)

type OrderIntent struct {
	ClientOrderID                string
	Provider                     string
	Mode                         string
	AccountRef                   string
	Symbol                       string
	Exchange                     string
	Side                         string
	OrderType                    string
	Quantity                     string
	LimitPrice                   string
	Currency                     string
	StrategyResultSHA256         string
	StrategySelectionEventID     string
	SignalSchemaVersion          string
	SignalID                     string
	SignalDataSHA256             string
	SignalDataAsOf               string
	SignalGeneratedAt            string
	SignalExpiresAt              string
	SignalTargetQuantity         string
	PaperAccountingSessionID     string
	PaperAccountingPolicyVersion string
	PaperSignalEventID           string
	ExecutionPolicySHA256        string
}

func ValidateOrderIntent(intent OrderIntent) error {
	if !SafeOrderID(intent.ClientOrderID) {
		return errors.New("client_order_id is invalid")
	}
	if intent.Provider != "kiwoom" || (intent.Mode != "synthetic" && intent.Mode != "paper") || intent.Exchange != "KRX" ||
		(intent.Side != "BUY" && intent.Side != "SELL") || intent.Currency != "KRW" {
		return errors.New("order intent is outside the synthetic/paper Kiwoom/KRW/KRX boundary")
	}
	if !OrderAlias(intent.AccountRef, "account") {
		return errors.New("account_ref must be an opaque Kiwoom account alias")
	}
	if !kiwoomStockPattern.MatchString(intent.Symbol) {
		return errors.New("symbol must be a six-digit KRX code")
	}
	if !validOrderInteger(intent.Quantity) {
		return errors.New("quantity must be a positive canonical integer")
	}
	capitalized := intent.Mode == "paper" && intent.SignalSchemaVersion == capitalizedPaperSignalSchema
	if capitalized {
		if intent.OrderType != "PAPER_MARKET" || intent.LimitPrice != "" || !paperdomain.ValidCapitalizedQuantity(intent.Quantity, false) {
			return errors.New("capitalized paper order must be PAPER_MARKET without a limit price")
		}
	} else {
		if intent.OrderType != "LIMIT" || len(intent.LimitPrice) == 0 || len(intent.LimitPrice) > 64 {
			return errors.New("limit_price is invalid")
		}
		price, err := exact.ParseDecimal(intent.LimitPrice)
		if err != nil || price.Sign() <= 0 {
			return errors.New("limit_price must be a positive canonical decimal")
		}
	}
	strategyBound := intent.StrategyResultSHA256 != "" || intent.StrategySelectionEventID != ""
	if strategyBound && (!strategySHA256Pattern.MatchString(intent.StrategyResultSHA256) || !SafeOrderID(intent.StrategySelectionEventID)) {
		return errors.New("strategy order selection binding is invalid")
	}
	signalBound := intent.SignalSchemaVersion != "" || intent.SignalID != "" || intent.SignalDataSHA256 != "" ||
		intent.SignalDataAsOf != "" || intent.SignalGeneratedAt != "" || intent.SignalExpiresAt != "" || intent.SignalTargetQuantity != ""
	if intent.Mode == "paper" && (!strategyBound || !signalBound) {
		return errors.New("paper order requires strategy and signal bindings")
	}
	if signalBound {
		dataAsOf, dataOK := CanonicalUTCTime(intent.SignalDataAsOf)
		generatedAt, generatedOK := CanonicalUTCTime(intent.SignalGeneratedAt)
		expiresAt, expiresOK := CanonicalUTCTime(intent.SignalExpiresAt)
		legacySignal := intent.SignalSchemaVersion == legacyPaperSignalSchema && intent.SignalTargetQuantity == ""
		targetSignal := intent.SignalSchemaVersion == paperSignalSchema && validOrderInteger(intent.SignalTargetQuantity)
		if targetSignal {
			target, _ := exact.ParseDecimal(intent.SignalTargetQuantity)
			quantity, _ := exact.ParseDecimal(intent.Quantity)
			targetSignal = target.Cmp(quantity) >= 0
		}
		capitalizedSignal := intent.SignalSchemaVersion == capitalizedPaperSignalSchema && validPaperTargetQuantity(intent.SignalTargetQuantity)
		if capitalizedSignal {
			dataAsOf, dataOK = parsePaperTime(intent.SignalDataAsOf)
			generatedAt, generatedOK = parsePaperTime(intent.SignalGeneratedAt)
			expiresAt, expiresOK = parsePaperTime(intent.SignalExpiresAt)
		}
		if !strategyBound || (!legacySignal && !targetSignal && !capitalizedSignal) || !SafeOrderID(intent.SignalID) ||
			!strategySHA256Pattern.MatchString(intent.SignalDataSHA256) || !dataOK || !generatedOK || !expiresOK ||
			dataAsOf.After(generatedAt) || !generatedAt.Before(expiresAt) {
			return errors.New("strategy signal binding is invalid")
		}
	}
	paperBound := intent.PaperAccountingSessionID != "" || intent.PaperAccountingPolicyVersion != "" ||
		intent.PaperSignalEventID != "" || intent.ExecutionPolicySHA256 != ""
	if capitalized {
		if !SafeOrderID(intent.PaperAccountingSessionID) || intent.PaperAccountingPolicyVersion != paperAccountingPolicyVersion ||
			!SafeOrderID(intent.PaperSignalEventID) || !strategySHA256Pattern.MatchString(intent.ExecutionPolicySHA256) {
			return errors.New("capitalized paper order accounting binding is invalid")
		}
	} else if paperBound {
		return errors.New("paper accounting binding is invalid for a legacy or synthetic order")
	}
	return nil
}

func validOrderInteger(raw string) bool {
	return len(raw) <= 32 && orderIntegerPattern.MatchString(raw)
}

func validPaperTargetQuantity(raw string) bool {
	return paperdomain.ValidCapitalizedQuantity(raw, true)
}

func canonicalPaperTime(raw string) (string, bool) {
	value, ok := parsePaperTime(raw)
	if !ok {
		return "", false
	}
	return value.Format(canonicalPaperTimeLayout), true
}

func parsePaperTime(raw string) (time.Time, bool) {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || value.Location() != time.UTC {
		return time.Time{}, false
	}
	return value, value.Format(time.RFC3339Nano) == raw || value.Format(canonicalPaperTimeLayout) == raw
}

const (
	capitalizedPaperSignalSchema = "paper-signal.v3"
	legacyPaperSignalSchema      = "paper-signal.v1"
	paperSignalSchema            = "paper-signal.v2"
	paperAccountingPolicyVersion = "paper_accounting_v1"
	canonicalPaperTimeLayout     = "2006-01-02T15:04:05.000000000Z"
)

var (
	kiwoomStockPattern    = regexp.MustCompile(`^[0-9]{6}$`)
	strategySHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	orderIntegerPattern   = regexp.MustCompile(`^[1-9][0-9]*$`)
)
