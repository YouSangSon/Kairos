package sqlite

import (
	"omni-folio/services/core/internal/modules/trading/internal/application"

	"omni-folio/services/core/internal/modules/trading/contracts"
)

const paperPromotionPolicyVersion = "paper-forward-comparison.v1"

type paperPromotionComparison = contracts.PaperPromotionComparison

type paperPromotionWindow = contracts.PaperPromotionWindow

var parsePaperPromotionBars = application.ParsePaperPromotionBars

var paperPromotionStrategy = application.PaperPromotionStrategy

var comparePaperPromotion = application.ComparePaperPromotion

var evaluatePaperPromotionWindows = application.EvaluatePaperPromotionWindows

var paperPromotionMetricOrder = application.PaperPromotionMetricOrder
