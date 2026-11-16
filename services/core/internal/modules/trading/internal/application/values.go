package application

import (
	"omni-folio/services/core/internal/modules/trading/contracts"
)

type ExecutionAuthorityState = contracts.ExecutionAuthorityState
type KiwoomOrderLookup = contracts.OrderLookup
type KiwoomOrderObservation = contracts.OrderObservation
type KiwoomExecutionObservation = contracts.ExecutionObservation
type KiwoomOrderReconciliation = contracts.OrderReconciliation
type LocalOrderLog = contracts.LocalOrderLog
type LocalOrderView = contracts.LocalOrderView
type OrderIntent = contracts.OrderIntent
type OrderEvent = contracts.OrderEvent
type OrderState = contracts.OrderState
type PaperAccountingSession = contracts.PaperAccountingSession
type PaperEvaluationEvent = contracts.PaperEvaluationEvent
type PaperImprovementReceipt = contracts.PaperImprovementReceipt
type PaperInputStatus = contracts.PaperInputStatus
type LocalPaperStepResult = contracts.LocalPaperStepResult
type PaperMarketBarObservation = contracts.PaperMarketBarObservation
type PaperSignalEvent = contracts.PaperSignalEvent
type PaperMonitor = contracts.PaperMonitor
type PaperMonitorRunner = contracts.PaperMonitorRunner
type PaperMonitorPolicy = contracts.PaperMonitorPolicy
type PaperPerformanceEvent = contracts.PaperPerformanceEvent
type PaperPerformancePolicyEvent = contracts.PaperPerformancePolicyEvent
type PaperPromotionReceipt = contracts.PaperPromotionReceipt
type PaperPromotionReportView = contracts.PaperPromotionReportView
type StoredPaperPromotionReport = contracts.StoredPaperPromotionReport
type PaperPromotionReportStrategy = contracts.PaperPromotionReportStrategy
type PaperPromotionReportComparison = contracts.PaperPromotionReportComparison
type PaperPromotionReportWindow = contracts.PaperPromotionReportWindow
type PaperPromotionReportMetrics = contracts.PaperPromotionReportMetrics
type PaperSignal = contracts.PaperSignal
type PaperMarketObservation = contracts.PaperMarketObservation
type PaperScheduledRunResult = contracts.PaperScheduledRunResult
type PaperSnapshotImport = contracts.PaperSnapshotImport
type PaperStrategyPerformanceEvent = contracts.PaperStrategyPerformanceEvent
type StrategySelectionEvent = contracts.StrategySelectionEvent
type StrategySelectionState = contracts.StrategySelectionState
