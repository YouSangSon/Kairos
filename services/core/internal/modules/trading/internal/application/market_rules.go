package application

import (
	"omni-folio/services/core/internal/modules/trading/internal/domain"
	"time"
)

func ValidatePaperMarketBar(bar PaperMarketBarObservation) error {
	return domain.ValidatePaperMarketBar(domain.PaperMarketBarObservation(bar))
}

var validatePaperMarketBar = ValidatePaperMarketBar

func ValidateCapitalizedPaperSignal(signal PaperSignal, now time.Time) error {
	return domain.ValidateCapitalizedPaperSignal(domain.PaperSignal(signal), now)
}

var validateCapitalizedPaperSignal = ValidateCapitalizedPaperSignal

func ValidateCapitalizedPaperSignalShape(signal PaperSignal) error {
	return domain.ValidateCapitalizedPaperSignalShape(domain.PaperSignal(signal))
}

var validateCapitalizedPaperSignalShape = ValidateCapitalizedPaperSignalShape

func ValidatePaperSignalEvent(event PaperSignalEvent) error {
	return domain.ValidatePaperSignalEvent(domain.PaperSignalEvent(event))
}

var validatePaperSignalEvent = ValidatePaperSignalEvent

func SamePaperMarketBarInput(left, right PaperMarketBarObservation) bool {
	return domain.SamePaperMarketBarInput(domain.PaperMarketBarObservation(left), domain.PaperMarketBarObservation(right))
}

var samePaperMarketBarInput = SamePaperMarketBarInput

func SamePaperSignalInput(event PaperSignalEvent, signal PaperSignal) bool {
	return domain.SamePaperSignalInput(domain.PaperSignalEvent(event), domain.PaperSignal(signal))
}

var samePaperSignalInput = SamePaperSignalInput
var ValidPaperTargetQuantity = domain.ValidPaperTargetQuantity
var validPaperTargetQuantity = ValidPaperTargetQuantity
var CanonicalPaperTime = domain.CanonicalPaperTime
var canonicalPaperTime = CanonicalPaperTime
var ParsePaperTime = domain.ParsePaperTime
var parsePaperTime = ParsePaperTime
var CanonicalPaperTimes = domain.CanonicalPaperTimes
var canonicalPaperTimes = CanonicalPaperTimes
var PaperMarketBarObservationID = domain.PaperMarketBarObservationID
var paperMarketBarObservationID = PaperMarketBarObservationID
var PaperSignalEventID = domain.PaperSignalEventID
var paperSignalEventID = PaperSignalEventID
