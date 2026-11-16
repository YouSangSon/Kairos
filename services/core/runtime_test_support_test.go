package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"omni-folio/services/core/internal/codec"
	"omni-folio/services/core/internal/composition"
	"omni-folio/services/core/internal/exact"
	ledgermodule "omni-folio/services/core/internal/modules/ledger"
	ledgercontracts "omni-folio/services/core/internal/modules/ledger/contracts"
	maintenancecontracts "omni-folio/services/core/internal/modules/maintenance/contracts"
	marketmodule "omni-folio/services/core/internal/modules/marketdata"
	marketdatacontracts "omni-folio/services/core/internal/modules/marketdata/contracts"
	portfoliocontracts "omni-folio/services/core/internal/modules/portfolio/contracts"
	research "omni-folio/services/core/internal/modules/research"
	researchcontracts "omni-folio/services/core/internal/modules/research/contracts"
	tradingmodule "omni-folio/services/core/internal/modules/trading"
	tradingcontracts "omni-folio/services/core/internal/modules/trading/contracts"
	watchlistcontracts "omni-folio/services/core/internal/modules/watchlist/contracts"
	storagecontracts "omni-folio/services/core/internal/storage/contracts"
	"omni-folio/services/core/internal/storage/observation"
	pool "omni-folio/services/core/internal/storage/sqlite"
	"omni-folio/services/core/migrations"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

type ApplyReceipt = ledgercontracts.ApplyReceipt
type ApplyRequest = ledgercontracts.ApplyRequest
type BackupEncryption = maintenancecontracts.BackupEncryption
type BackupManifest = maintenancecontracts.BackupManifest
type BenchmarkIndexPoint = marketdatacontracts.BenchmarkIndexPoint
type BenchmarkSeriesInput = marketdatacontracts.BenchmarkSeriesInput
type BenchmarkSeriesReceipt = marketdatacontracts.BenchmarkSeriesReceipt
type BrokerKnownGoodSnapshot = ledgercontracts.BrokerKnownGoodSnapshot
type BrokerPositionDifference = ledgercontracts.BrokerPositionDifference
type CorrectionTarget = ledgercontracts.CorrectionTarget
type ExecutionAuthorityState = tradingcontracts.ExecutionAuthorityState
type FXObservation = marketdatacontracts.FXObservation
type FXObservationInput = marketdatacontracts.FXObservationInput
type Holding = ledgercontracts.Holding
type ImportPreview = ledgercontracts.ImportPreview
type InstrumentLedger = ledgercontracts.InstrumentLedger
type InstrumentListingEvent = marketdatacontracts.InstrumentListingEvent
type InstrumentListingInput = marketdatacontracts.InstrumentListingInput
type LedgerActivity = ledgercontracts.LedgerActivity
type LedgerActivityPage = ledgercontracts.LedgerActivityPage
type LocalOrderLog = tradingcontracts.LocalOrderLog
type LocalPaperStepResult = tradingcontracts.LocalPaperStepResult
type MarketDataBar = marketdatacontracts.MarketDataBar
type MarketDataSeries = marketdatacontracts.MarketDataSeries
type Money = ledgercontracts.Money
type OrderEvent = tradingcontracts.OrderEvent
type OrderIntent = tradingcontracts.OrderIntent
type OrderState = tradingcontracts.OrderState
type OwnerFXPoint = marketdatacontracts.OwnerFXPoint
type OwnerFXSeriesInput = marketdatacontracts.OwnerFXSeriesInput
type OwnerFXSeriesReceipt = marketdatacontracts.OwnerFXSeriesReceipt
type OwnerPricePoint = marketdatacontracts.OwnerPricePoint
type OwnerPriceSeriesInput = marketdatacontracts.OwnerPriceSeriesInput
type OwnerPriceSeriesReceipt = marketdatacontracts.OwnerPriceSeriesReceipt
type PaperAccountingSession = tradingcontracts.PaperAccountingSession
type PaperEvaluationEvent = tradingcontracts.PaperEvaluationEvent
type PaperImprovementReceipt = tradingcontracts.PaperImprovementReceipt
type PaperInputStatus = tradingcontracts.PaperInputStatus
type PaperMarketBarObservation = tradingcontracts.PaperMarketBarObservation
type PaperMarketObservation = tradingcontracts.PaperMarketObservation
type PaperMonitor = tradingcontracts.PaperMonitor
type PaperPerformanceEvent = tradingcontracts.PaperPerformanceEvent
type PaperPerformancePolicyEvent = tradingcontracts.PaperPerformancePolicyEvent
type PaperPromotionReceipt = tradingcontracts.PaperPromotionReceipt
type PaperPromotionReportView = tradingcontracts.PaperPromotionReportView
type PaperScheduledRunResult = tradingcontracts.PaperScheduledRunResult
type PaperSignal = tradingcontracts.PaperSignal
type PaperSignalEvent = tradingcontracts.PaperSignalEvent
type PaperSnapshotImport = tradingcontracts.PaperSnapshotImport
type PaperStrategyPerformanceEvent = tradingcontracts.PaperStrategyPerformanceEvent
type PortfolioSnapshot = ledgercontracts.PortfolioSnapshot
type PreviewRow = ledgercontracts.PreviewRow
type Provenance = ledgercontracts.Provenance
type ResearchReportView = tradingcontracts.ResearchReportView
type Resolution = ledgercontracts.Resolution
type SecurityPriceObservation = marketdatacontracts.SecurityPriceObservation
type SecurityPriceObservationInput = marketdatacontracts.SecurityPriceObservationInput
type StrategyEvidence = tradingcontracts.StrategyEvidence
type StrategySelectionState = tradingcontracts.StrategySelectionState
type Transaction = ledgercontracts.Transaction
type VerificationReceipt = maintenancecontracts.VerificationReceipt
type APIError = ledgercontracts.APIError
type Service struct {
	*composition.Runtime
	db                               *sql.DB
	now                              func() time.Time
	id                               func(string) string
	ttl                              time.Duration
	marketData                       marketdatacontracts.CandleSource
	researchFeed                     string
	executionOwner, paperRunnerOwner string
}

func newService(db *sql.DB, now func() time.Time, id func(string) string) *Service {
	fixture := &Service{db: db, now: now, id: id, ttl: 15 * time.Minute}
	runtime, err := composition.Compose(composition.Config{DB: db, Now: func() time.Time { return fixture.now() }, ID: func(prefix string) string { return fixture.id(prefix) }, PreviewTTL: func() time.Duration { return fixture.ttl }})
	if err != nil {
		panic(err)
	}
	fixture.Runtime = runtime
	return fixture
}
func (s *Service) routes() http.Handler {
	if s.marketData != nil {
		s.Runtime.Market.Candles = s.marketData
	}
	s.Runtime.Research = research.Compose(research.Config{FeedPath: func() string { return s.researchFeed }, Now: func() time.Time { return s.now() }})
	return s.Runtime.Routes()
}
func openDB(path string) (*sql.DB, error)         { return pool.Open(path) }
func openExistingDB(path string) (*sql.DB, error) { return pool.OpenExisting(path) }
func migrate(db *sql.DB) error {
	return newService(db, time.Now, randomID).Maintenance.Migrations.Migrate(context.Background())
}
func requireSchema(db *sql.DB) error {
	return newService(db, time.Now, randomID).Maintenance.Schema.RequireCurrent(context.Background())
}
func requireServerStartupRecovery(db *sql.DB) error {
	return newService(db, time.Now, randomID).Maintenance.Schema.RequireStartup(context.Background())
}
func withCORS(next http.Handler, origin string) http.Handler {
	return composition.WithCORS(next, origin)
}
func validateAllowedOrigin(origin string) error { return composition.ValidateAllowedOrigin(origin) }

var migrationFiles = migrations.Files

const latestSchema = 27
const zeroTime = "1970-01-01T00:00:00Z"
const fifoCostBasisPolicy = ledgercontracts.FIFOCostBasisPolicy
const backupFormat = "omni-folio-backup.v21"
const backupSchema = "omni-folio.sqlite.v27"
const legacyFIFOCostBasisPolicy = "fifo_exact_else_half_even_residual_8_v1"

func randomID(prefix string) string { return composition.NewResources().ID(prefix) }

type appError struct {
	Status int
	Body   APIError
}

func (e *appError) Error() string { return e.Body.Message }
func testLedgerError(err *ledgercontracts.Error) *appError {
	if err == nil {
		return nil
	}
	status := http.StatusInternalServerError
	switch err.Kind {
	case ledgercontracts.InvalidInput:
		status = http.StatusBadRequest
	case ledgercontracts.NotFound:
		status = http.StatusNotFound
	case ledgercontracts.Conflict:
		status = http.StatusConflict
	}
	return &appError{Status: status, Body: err.Body}
}
func (s *Service) Preview(ctx context.Context, raw []byte) (*ImportPreview, *appError) {
	result, err := s.Ledger.PreviewImport(ctx, raw)
	return result, testLedgerError(err)
}
func (s *Service) Apply(ctx context.Context, req ApplyRequest) (*ApplyReceipt, *appError) {
	result, err := s.Ledger.ApplyImport(ctx, req)
	return result, testLedgerError(err)
}
func (s *Service) recordKiwoomSnapshot(ctx context.Context, account string, snapshot *KiwoomSnapshot) (*BrokerKnownGoodSnapshot, error) {
	return s.Ledger.RecordBrokerSnapshot(ctx, account, snapshot)
}
func (s *Service) latestKiwoomSnapshot(ctx context.Context, environment KiwoomEnvironment, exchange KiwoomExchange, account string) (*BrokerKnownGoodSnapshot, error) {
	return s.Ledger.LatestBrokerSnapshot(ctx, environment, exchange, account)
}
func (s *Service) ledgerActivities(ctx context.Context, limit int, cursor *ledgerActivityCursor) (*LedgerActivityPage, error) {
	return s.Ledger.Activity(ctx, limit, cursor)
}
func (s *Service) instrumentLedger(ctx context.Context, id, currency string) (*InstrumentLedger, *appError) {
	result, err := s.Ledger.InstrumentActivity(ctx, id, currency)
	return result, testLedgerError(err)
}
func snapshotFrom(ctx context.Context, db *sql.DB) (*PortfolioSnapshot, error) {
	return newService(db, time.Now, randomID).Ledger.Snapshot(ctx)
}
func previewLedgerReplay(ctx context.Context, db *sql.DB) (*ledgerReplayPreview, error) {
	return newService(db, time.Now, randomID).Ledger.PreviewReplay(ctx)
}
func activateLedgerReplay(ctx context.Context, db *sql.DB, revision int64, events, snapshot string, now time.Time) (*ledgerReplayPreview, error) {
	return newService(db, func() time.Time { return now }, randomID).Ledger.ActivateReplay(ctx, revision, events, snapshot)
}
func createBackup(db *sql.DB, out, golden, manifest string, now func() time.Time, id func(string) string) (*BackupManifest, error) {
	return newService(db, now, id).Maintenance.Backups.CreateBackup(context.Background(), maintenancecontracts.BackupRequest{OutputPath: out, GoldenPath: golden, ManifestPath: manifest})
}
func backupAndVerify(db *sql.DB, out, golden string) error {
	_, err := createBackup(db, out, golden, out+".manifest.json", time.Now, randomID)
	return err
}
func verifyManifest(path, golden, manifest string) error {
	return composition.NewResources().Maintenance().VerifyManifest(context.Background(), path, golden, manifest)
}

type KiwoomSnapshot = ledgercontracts.BrokerSnapshot
type KiwoomEnvironment = ledgercontracts.BrokerEnvironment
type KiwoomExchange = ledgercontracts.BrokerExchange
type KiwoomTotals = ledgercontracts.BrokerTotals
type KiwoomPosition = ledgercontracts.BrokerPosition
type KiwoomOpenOrder = ledgercontracts.BrokerOpenOrder
type ledgerReplayPreview = ledgercontracts.ReplayPreview
type ledgerActivityCursor = ledgercontracts.ActivityCursor

const KiwoomMock KiwoomEnvironment = "mock"
const KiwoomProduction KiwoomEnvironment = "production"
const KiwoomKRX KiwoomExchange = "KRX"

func (s *Service) setSyntheticExecutionArmed(ctx context.Context, accountRef string, armed bool) (*ExecutionAuthorityState, error) {
	return s.Trading.SetSyntheticExecutionArmed(ctx, accountRef, armed)
}
func (s *Service) acquireSyntheticExecutionLease(ctx context.Context, accountRef string) (*ExecutionAuthorityState, error) {
	return s.Trading.AcquireSyntheticExecutionLease(ctx, accountRef)
}
func (s *Service) authorizeSyntheticDispatch(ctx context.Context, orderID string, fencingToken int64) (*OrderState, error) {
	return s.Trading.AuthorizeSyntheticDispatch(ctx, orderID, fencingToken)
}
func (s *Service) localOrderLog(ctx context.Context) (*LocalOrderLog, error) {
	return s.Trading.LocalOrderLog(ctx)
}
func (s *Service) recordOrderIntent(ctx context.Context, intent OrderIntent) (*OrderState, error) {
	return s.Trading.RecordOrderIntent(ctx, intent)
}
func (s *Service) appendOrderEvent(ctx context.Context, event OrderEvent) (*OrderState, error) {
	return s.Trading.AppendOrderEvent(ctx, event)
}
func (s *Service) loadOrderState(ctx context.Context, orderID string) (*OrderState, error) {
	return s.Trading.LoadOrderState(ctx, orderID)
}
func (s *Service) openPaperAccountingSession(ctx context.Context, accountRef, resultSHA256, selectionEventID string) (*PaperAccountingSession, error) {
	return s.Trading.OpenPaperAccountingSession(ctx, accountRef, resultSHA256, selectionEventID)
}
func (s *Service) evaluatePaperOperations(ctx context.Context, accountRef, resultSHA256, selectionEventID string) (*PaperEvaluationEvent, error) {
	return s.Trading.EvaluatePaperOperations(ctx, accountRef, resultSHA256, selectionEventID)
}
func (s *Service) improvePaperCandidate(ctx context.Context, account, attemptID string, artifact, raw []byte) (*PaperImprovementReceipt, error) {
	return s.Trading.ImprovePaperCandidate(ctx, account, attemptID, artifact, raw)
}
func (s *Service) paperInputStatus(ctx context.Context, account, selection string, raw []byte) (*PaperInputStatus, error) {
	return s.Trading.PaperInputStatus(ctx, account, selection, raw)
}
func (s *Service) executeLocalPaper(ctx context.Context, account, selection string, proposalRaw, barsRaw, researchRaw []byte) (result *LocalPaperStepResult, resultErr error) {
	return s.Trading.ExecuteLocalPaper(ctx, account, selection, proposalRaw, barsRaw, researchRaw)
}
func (s *Service) recordPaperMarketBar(ctx context.Context, input PaperMarketBarObservation) (*PaperMarketBarObservation, error) {
	return s.Trading.RecordPaperMarketBar(ctx, input)
}
func (s *Service) paperMonitor(ctx context.Context) (*PaperMonitor, error) {
	return s.Trading.PaperMonitor(ctx)
}
func (s *Service) evaluatePaperPerformance(ctx context.Context, accountRef, asOf string) (*PaperPerformanceEvent, error) {
	return s.Trading.EvaluatePaperPerformance(ctx, accountRef, asOf)
}
func (s *Service) applyPaperPerformancePolicy(ctx context.Context, accountRef, expectedSelectionEventID, expectedStrategyPerformanceID string) (*PaperPerformancePolicyEvent, error) {
	return s.Trading.ApplyPaperPerformancePolicy(ctx, accountRef, expectedSelectionEventID, expectedStrategyPerformanceID)
}
func (s *Service) promotePaperCandidate(ctx context.Context, account, candidateSHA, expectedSelection, attemptID string, raw []byte) (*PaperPromotionReceipt, error) {
	return s.Trading.PromotePaperCandidate(ctx, account, candidateSHA, expectedSelection, attemptID, raw)
}
func (s *Service) paperPromotionStatus(ctx context.Context, attemptID string) (*PaperPromotionReceipt, error) {
	return s.Trading.PaperPromotionStatus(ctx, attemptID)
}
func (s *Service) abortPaperPromotion(ctx context.Context, attemptID string) (*PaperPromotionReceipt, error) {
	return s.Trading.AbortPaperPromotion(ctx, attemptID)
}
func (s *Service) paperPromotionReport(ctx context.Context) (*PaperPromotionReportView, error) {
	return s.Trading.PaperPromotionReport(ctx)
}
func (s *Service) admitPaperProposal(ctx context.Context, accountRef, selectionEventID string, raw []byte, barID string, fencingToken int64) (*PaperSignalEvent, *OrderState, error) {
	return s.Trading.AdmitPaperProposal(ctx, accountRef, selectionEventID, raw, barID, fencingToken)
}
func (s *Service) runPaperSignal(ctx context.Context, accountRef string, signal PaperSignal, observation PaperMarketObservation, fencingToken int64) (*OrderState, error) {
	return s.Trading.RunPaperSignal(ctx, accountRef, signal, observation, fencingToken)
}
func (s *Service) admitPaperSignal(ctx context.Context, accountRef string, signal PaperSignal, fencingToken int64) (*PaperSignalEvent, *OrderState, error) {
	return s.Trading.AdmitPaperSignal(ctx, accountRef, signal, fencingToken)
}
func (s *Service) runPaperOrder(ctx context.Context, orderID string, fencingToken int64) (*OrderState, error) {
	return s.Trading.RunPaperOrder(ctx, orderID, fencingToken)
}
func (s *Service) runPaperPerformanceLoop(ctx context.Context, accountRef string) error {
	return s.Trading.RunPaperPerformanceLoop(ctx, accountRef)
}
func (s *Service) runDuePaperPerformancePolicy(ctx context.Context, accountRef string) (*PaperScheduledRunResult, error) {
	return s.Trading.RunDuePaperPerformancePolicy(ctx, accountRef)
}
func (s *Service) importPaperSnapshot(ctx context.Context, raw []byte) (*PaperSnapshotImport, error) {
	return s.Trading.ImportPaperSnapshot(ctx, raw)
}
func (s *Service) evaluatePaperStrategyPerformance(
	ctx context.Context,
	accountRef string,
	expectedSelectionEventID string,
	expectedLatestPerformanceID string,
) (*PaperStrategyPerformanceEvent, error) {
	return s.Trading.EvaluatePaperStrategyPerformance(ctx, accountRef, expectedSelectionEventID, expectedLatestPerformanceID)
}
func (s *Service) researchReport(ctx context.Context) (*ResearchReportView, error) {
	return s.Trading.ResearchReport(ctx)
}
func (s *Service) registerStrategyEvidence(ctx context.Context, artifact []byte) (*StrategyEvidence, error) {
	return s.Trading.RegisterEvidence(ctx, artifact)
}
func (s *Service) selectPaperCandidate(ctx context.Context, resultSHA256, expectedCurrentEventID string) (*StrategySelectionState, error) {
	return s.Trading.SelectPaperCandidate(ctx, resultSHA256, expectedCurrentEventID)
}
func (s *Service) rollbackPaperCandidate(ctx context.Context, expectedCurrentEventID, sourceEventID string) (*StrategySelectionState, error) {
	return s.Trading.RollbackPaperCandidate(ctx, expectedCurrentEventID, sourceEventID)
}

func (s *Service) recordFXObservation(ctx context.Context, input FXObservationInput) (*FXObservation, error) {
	return s.Market.Observations.RecordFX(ctx, input)
}
func (s *Service) latestFXObservation(ctx context.Context, source, base, quote, asof string) (*FXObservation, error) {
	return s.Market.Observations.LatestFX(ctx, source, base, quote, asof)
}
func (s *Service) recordSecurityPriceObservation(ctx context.Context, input SecurityPriceObservationInput) (*SecurityPriceObservation, error) {
	return s.Market.Observations.RecordPrice(ctx, input)
}
func (s *Service) declareInstrumentListing(ctx context.Context, input InstrumentListingInput) (*InstrumentListingEvent, error) {
	return s.Market.Observations.DeclareListing(ctx, input)
}
func (s *Service) revokeInstrumentListing(ctx context.Context, input InstrumentListingInput) (*InstrumentListingEvent, error) {
	return s.Market.Observations.RevokeListing(ctx, input)
}
func (s *Service) resolveInstrumentListing(ctx context.Context, symbol, venue, currency string) (*InstrumentListingEvent, error) {
	return s.Market.Observations.ResolveListing(ctx, symbol, venue, currency)
}
func (s *Service) importOwnerFXSeries(ctx context.Context, raw []byte) (*OwnerFXSeriesReceipt, error) {
	return s.Market.Imports.ImportOwnerFX(ctx, raw)
}
func (s *Service) importBenchmarkSeries(ctx context.Context, raw []byte) (*BenchmarkSeriesReceipt, error) {
	return s.Market.Imports.ImportBenchmark(ctx, raw)
}
func (s *Service) importOwnerPriceSeries(ctx context.Context, raw []byte) (*OwnerPriceSeriesReceipt, error) {
	return s.OwnerPrice.ImportOwnerPrice(ctx, raw)
}
func (s *Service) cashValuation(ctx context.Context, currency, at string) (*CashValuation, error) {
	return s.Portfolio.CashValuation(ctx, currency, at)
}
func (s *Service) holdingValuation(ctx context.Context, at string) (*HoldingValuation, error) {
	return s.Portfolio.HoldingValuation(ctx, at)
}
func (s *Service) currentPortfolioValuation(ctx context.Context) (*CurrentPortfolioValuation, error) {
	return s.Portfolio.CurrentValuation(ctx)
}
func (s *Service) portfolioXIRR(ctx context.Context) (*PortfolioXIRR, error) {
	return s.Portfolio.XIRR(ctx)
}
func (s *Service) portfolioTWR(ctx context.Context) (*PortfolioTWR, error) {
	return s.Portfolio.TWR(ctx)
}
func (s *Service) portfolioTWRCheckpoints(ctx context.Context) (*PortfolioTWRCheckpoints, error) {
	return s.Portfolio.TWRCheckpoints(ctx)
}
func (s *Service) portfolioBenchmarks(ctx context.Context) (*PortfolioBenchmarks, error) {
	return s.Portfolio.Benchmarks(ctx)
}
func (s *Service) watchlist(ctx context.Context) (*WatchlistReport, error) {
	return s.Watchlist.API.Read(ctx)
}
func (s *Service) replaceWatchlist(ctx context.Context, input WatchlistChange) (*WatchlistState, error) {
	return s.Watchlist.API.Replace(ctx, input)
}

type Error = ledgercontracts.Error
type BrokerRecovery = ledgercontracts.BrokerRecovery
type ActivityCursor = ledgercontracts.ActivityCursor
type ExportReceipt = ledgercontracts.ExportReceipt
type ReplayPreview = ledgercontracts.ReplayPreview
type Imports = ledgercontracts.Imports
type Queries = ledgercontracts.Queries
type Maintenance = ledgercontracts.Maintenance
type Status = ledgercontracts.Status
type BrokerTotals = ledgercontracts.BrokerTotals
type BrokerPosition = ledgercontracts.BrokerPosition
type BrokerOpenOrder = ledgercontracts.BrokerOpenOrder
type BrokerSnapshot = ledgercontracts.BrokerSnapshot
type BrokerEnvironment = ledgercontracts.BrokerEnvironment
type BrokerExchange = ledgercontracts.BrokerExchange
type ValuationInputs = marketdatacontracts.ValuationInputs
type ValuationQuery = marketdatacontracts.ValuationQuery
type Observations = marketdatacontracts.Observations
type CandleSource = marketdatacontracts.CandleSource
type RecoveryProof = marketdatacontracts.RecoveryProof
type BackupQuery = marketdatacontracts.BackupQuery
type Recovery = marketdatacontracts.Recovery
type Failure = portfoliocontracts.Failure
type HoldingValuationPrice = portfoliocontracts.HoldingValuationPrice
type HoldingValuationLine = portfoliocontracts.HoldingValuationLine
type HoldingValuationTotal = portfoliocontracts.HoldingValuationTotal
type HoldingValuation = portfoliocontracts.HoldingValuation
type CashValuation = portfoliocontracts.CashValuation
type CurrentPortfolioValuation = portfoliocontracts.CurrentPortfolioValuation
type PortfolioTWR = portfoliocontracts.PortfolioTWR
type PortfolioTWRCheckpoints = portfoliocontracts.PortfolioTWRCheckpoints
type PortfolioXIRR = portfoliocontracts.PortfolioXIRR
type BenchmarkRateResult = portfoliocontracts.BenchmarkRateResult
type BenchmarkFXMark = portfoliocontracts.BenchmarkFXMark
type PortfolioBenchmark = portfoliocontracts.PortfolioBenchmark
type PortfolioBenchmarks = portfoliocontracts.PortfolioBenchmarks
type RecoverySnapshot = tradingcontracts.RecoverySnapshot
type OrderRecovery = tradingcontracts.OrderRecovery
type StrategyRecovery = tradingcontracts.StrategyRecovery
type PaperAccountingRecovery = tradingcontracts.PaperAccountingRecovery
type PaperPerformanceRecovery = tradingcontracts.PaperPerformanceRecovery
type PaperStrategyPerformanceRecovery = tradingcontracts.PaperStrategyPerformanceRecovery
type PaperPerformancePolicyRecovery = tradingcontracts.PaperPerformancePolicyRecovery
type RunnerLeaseRecovery = tradingcontracts.RunnerLeaseRecovery
type PromotionRecovery = tradingcontracts.PromotionRecovery
type MigrationQuery = tradingcontracts.MigrationQuery
type MigrationSnapshot = tradingcontracts.MigrationSnapshot
type Trading = tradingcontracts.Trading
type Orders = tradingcontracts.Orders
type Migrations = maintenancecontracts.Migrations
type BackupRequest = maintenancecontracts.BackupRequest
type Backups = maintenancecontracts.Backups
type Schema = maintenancecontracts.Schema
type WatchlistEntry = watchlistcontracts.WatchlistEntry
type WatchlistState = watchlistcontracts.WatchlistState
type WatchlistChange = watchlistcontracts.WatchlistChange
type WatchlistReport = watchlistcontracts.WatchlistReport
type API = watchlistcontracts.API

func orderJSONHash(value any) ([]byte, string, error) { return codec.JSONHash(value) }
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}
func copyFile(source, target string) (resultErr error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, in.Close()) }()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, out.Close()) }()
	_, err = io.Copy(out, in)
	return err
}

func verifyRestore(path, golden string) error {
	resource, err := composition.NewResources().Maintenance().OpenExisting(path)
	if err != nil {
		return err
	}
	defer resource.Close()
	return resource.Backups().VerifyRestore(context.Background(), path, golden)
}
func verifyRestoreProof(path, golden string) (struct{}, error) {
	return struct{}{}, verifyRestore(path, golden)
}

type orderRecoveryProof = tradingcontracts.OrderRecovery
type strategyRecoveryProof = tradingcontracts.StrategyRecovery
type paperAccountingRecoveryProof = tradingcontracts.PaperAccountingRecovery
type paperPerformanceRecoveryProof = tradingcontracts.PaperPerformanceRecovery
type paperStrategyPerformanceRecoveryProof = tradingcontracts.PaperStrategyPerformanceRecovery
type paperPerformancePolicyRecoveryProof = tradingcontracts.PaperPerformancePolicyRecovery
type paperRunnerLeaseRecoveryProof = tradingcontracts.RunnerLeaseRecovery
type paperPromotionRecoveryProof = tradingcontracts.PromotionRecovery

func proveOrderRecovery(ctx context.Context, db *sql.DB) (orderRecoveryProof, error) {
	proof, err := tradingRecoveryForTest(ctx, db, false)
	if err != nil {
		return orderRecoveryProof{}, err
	}
	return proof.Orders, nil
}
func proveStrategyRegistryRecovery(ctx context.Context, db *sql.DB) (strategyRecoveryProof, error) {
	proof, err := tradingRecoveryForTest(ctx, db, false)
	if err != nil {
		return strategyRecoveryProof{}, err
	}
	return proof.Strategy, nil
}
func provePaperAccountingRecovery(ctx context.Context, db *sql.DB) (paperAccountingRecoveryProof, error) {
	proof, err := tradingRecoveryForTest(ctx, db, false)
	if err != nil {
		return paperAccountingRecoveryProof{}, err
	}
	return proof.Accounting, nil
}
func provePaperPerformanceRecovery(ctx context.Context, db *sql.DB) (paperPerformanceRecoveryProof, error) {
	proof, err := tradingRecoveryForTest(ctx, db, false)
	if err != nil {
		return paperPerformanceRecoveryProof{}, err
	}
	return proof.Performance, nil
}
func provePaperStrategyPerformanceRecovery(ctx context.Context, db *sql.DB) (paperStrategyPerformanceRecoveryProof, error) {
	proof, err := tradingRecoveryForTest(ctx, db, false)
	if err != nil {
		return paperStrategyPerformanceRecoveryProof{}, err
	}
	return proof.StrategyPerformance, nil
}
func provePaperPerformancePolicyRecovery(ctx context.Context, db *sql.DB) (paperPerformancePolicyRecoveryProof, error) {
	proof, err := tradingRecoveryForTest(ctx, db, false)
	if err != nil {
		return paperPerformancePolicyRecoveryProof{}, err
	}
	return proof.Policy, nil
}
func provePaperRunnerLeaseRecovery(ctx context.Context, db *sql.DB) (paperRunnerLeaseRecoveryProof, error) {
	proof, err := tradingRecoveryForTest(ctx, db, false)
	if err != nil {
		return paperRunnerLeaseRecoveryProof{}, err
	}
	return proof.Runner, nil
}
func provePaperPromotionRecovery(ctx context.Context, db *sql.DB) (paperPromotionRecoveryProof, error) {
	proof, err := tradingRecoveryForTest(ctx, db, false)
	if err != nil {
		return paperPromotionRecoveryProof{}, err
	}
	return proof.Promotion, nil
}

func ledgerProofScope(ctx context.Context, db *sql.DB, work func(ledgercontracts.Service, storagecontracts.ObservationRef) error) error {
	registry := observation.New(db, time.Now)
	api, err := ledgermodule.Compose(ledgermodule.Config{DB: db, Observations: registry, Now: time.Now, ID: randomID, PreviewTTL: func() time.Duration { return 15 * time.Minute }})
	if err != nil {
		return err
	}
	return registry.VerifyBackup(ctx, func(ref storagecontracts.ObservationRef, _ time.Time) error { return work(api, ref) })
}
func proveBrokerRecovery(ctx context.Context, db *sql.DB) (ledgercontracts.BrokerRecovery, error) {
	var proof ledgercontracts.BrokerRecovery
	err := ledgerProofScope(ctx, db, func(api ledgercontracts.Service, ref storagecontracts.ObservationRef) error {
		var err error
		proof, err = api.VerifyBrokerBackup(ctx, ref)
		return err
	})
	return proof, err
}
func proveLedgerReplayState(ctx context.Context, db *sql.DB, archival bool) error {
	return ledgerProofScope(ctx, db, func(api ledgercontracts.Service, ref storagecontracts.ObservationRef) error {
		if !archival {
			return api.VerifyStartup(ctx, ref)
		}
		_, err := api.VerifyBackup(ctx, ref, true)
		return err
	})
}
func marketRecovery(ctx context.Context, db *sql.DB, schema int) (marketdatacontracts.RecoveryProof, error) {
	registry := observation.New(db, time.Now)
	ledger, err := ledgermodule.Compose(ledgermodule.Config{DB: db, Observations: registry, Now: time.Now, ID: randomID, PreviewTTL: func() time.Duration { return 15 * time.Minute }})
	if err != nil {
		return marketdatacontracts.RecoveryProof{}, err
	}
	market, err := marketmodule.Compose(marketmodule.Config{DB: db, Observations: registry, Ledger: ledger, Now: time.Now, ID: randomID})
	if err != nil {
		return marketdatacontracts.RecoveryProof{}, err
	}
	var proof marketdatacontracts.RecoveryProof
	err = registry.VerifyBackup(ctx, func(ref storagecontracts.ObservationRef, _ time.Time) error {
		var err error
		proof, err = market.Recovery.VerifyBackup(ctx, ref, marketdatacontracts.BackupQuery{Schema: schema})
		return err
	})
	return proof, err
}

type seriesRecoveryProof struct {
	SHA256           string
	Versions, Points int
}

func proveBenchmarkSeriesRecovery(ctx context.Context, db *sql.DB) (seriesRecoveryProof, error) {
	proof, err := marketRecovery(ctx, db, latestSchema)
	return seriesRecoveryProof{SHA256: proof.BenchmarkSHA256, Versions: proof.BenchmarkVersions, Points: proof.BenchmarkPoints}, err
}
func proveOwnerFXSeriesRecovery(ctx context.Context, db *sql.DB) (seriesRecoveryProof, error) {
	proof, err := marketRecovery(ctx, db, latestSchema)
	return seriesRecoveryProof{SHA256: proof.OwnerFXSHA256, Versions: proof.OwnerFXVersions, Points: proof.OwnerFXPoints}, err
}
func proveOwnerPriceSeriesRecovery(ctx context.Context, db *sql.DB) (seriesRecoveryProof, error) {
	proof, err := marketRecovery(ctx, db, latestSchema)
	return seriesRecoveryProof{SHA256: proof.OwnerPriceSHA256, Versions: proof.OwnerPriceVersions, Points: proof.OwnerPricePoints}, err
}

const legacyPaperRunnerFormat = maintenancecontracts.LegacyPaperRunnerFormat
const legacyPaperRunnerSchema = maintenancecontracts.LegacyPaperRunnerSchema
const legacyPaperPerformancePolicyFormat = maintenancecontracts.LegacyPaperPerformancePolicyFormat
const legacyPaperPerformancePolicySchema = maintenancecontracts.LegacyPaperPerformancePolicySchema
const legacyStrategyPerformanceFormat = maintenancecontracts.LegacyStrategyPerformanceFormat
const legacyStrategyPerformanceSchema = maintenancecontracts.LegacyStrategyPerformanceSchema
const legacyPaperPerformanceBackupFormat = maintenancecontracts.LegacyPaperPerformanceBackupFormat
const legacyPaperPerformanceBackupSchema = maintenancecontracts.LegacyPaperPerformanceBackupSchema
const legacyPaperFillBackupFormat = maintenancecontracts.LegacyPaperFillBackupFormat
const legacyPaperFillBackupSchema = maintenancecontracts.LegacyPaperFillBackupSchema
const legacyPaperAccountingBackupFormat = maintenancecontracts.LegacyPaperAccountingBackupFormat
const legacyPaperAccountingBackupSchema = maintenancecontracts.LegacyPaperAccountingBackupSchema
const legacyBackupFormat = maintenancecontracts.LegacyBackupFormat
const legacyFXBackupFormat = maintenancecontracts.LegacyFXBackupFormat
const legacyPriceBackupFormat = maintenancecontracts.LegacyPriceBackupFormat
const legacyListingBackupFormat = maintenancecontracts.LegacyListingBackupFormat

func portfolioError(err error) *appError {
	var failure *portfoliocontracts.Failure
	if errors.As(err, &failure) {
		status := http.StatusBadRequest
		if failure.Conflict {
			status = http.StatusConflict
		}
		return &appError{Status: status, Body: APIError{Code: failure.Code, Message: failure.Message, Field: failure.Field}}
	}
	return &appError{Status: http.StatusInternalServerError, Body: APIError{Code: "internal_error", Message: "internal server error"}}
}

type PublicResearchView = researchcontracts.PublicResearchView

const publicResearchMaxBytes = 1 << 20
const maxBodyBytes = 1 << 20
const maxImportRows = 10_000
const csvSchema = "omni-folio.csv.v1"
const mappingSchema = "canonical-transaction.v4"
const maxMarketDataRows = 500
const maxMarketDataBytes = 1 << 20
const marketDataAdjustmentUnspecified = "unspecified"
const marketDataAdjustmentProviderAdjusted = "provider_adjusted"

var errMarketDataNotFound = marketdatacontracts.ErrMarketDataNotFound

func parseDecimal(raw string) (*big.Rat, error)    { return exact.ParseDecimal(raw) }
func formatDecimal(value *big.Rat) (string, error) { return exact.FormatDecimal(value) }
func quantizeHalfEven(value *big.Rat, scale int) *big.Rat {
	return exact.QuantizeHalfEven(value, scale)
}
func fifoCostAllocation(cost, take, quantity *big.Rat) (*big.Rat, error) {
	return exact.FIFOAllocation(cost, take, quantity)
}
func nullable(raw string) any {
	if raw == "" {
		return nil
	}
	return raw
}

func writeCurrentSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	snapshot, err := snapshotFrom(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "golden-snapshot.json")
	writeJSONFile(t, path, snapshot)
	return path
}
func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
func revision(value int64) string { return fmt.Sprintf("rev_%010d", value) }

func openReadOnlyExistingDB(ctx context.Context, path string) (*sql.DB, error) {
	return pool.OpenReadOnlyExisting(ctx, path)
}
func openReadWriteExistingDB(ctx context.Context, path string) (*sql.DB, error) {
	return pool.OpenReadWriteExisting(ctx, path)
}
func runBenchmarkSeriesCommand(ctx context.Context, command string, args []string, out io.Writer) error {
	return composition.RunCLI(ctx, append([]string{command}, args...), composition.CLIOptions{}, os.Stdin, out, io.Discard)
}
func runOwnerFXImportCommand(ctx context.Context, args []string, out io.Writer) error {
	return runBenchmarkSeriesCommand(ctx, "owner-fx-import", args, out)
}
func runOwnerPriceImportCommand(ctx context.Context, args []string, out io.Writer) error {
	return runBenchmarkSeriesCommand(ctx, "owner-price-import", args, out)
}
func runPaperPromotionCommand(ctx context.Context, command string, args []string, out io.Writer) error {
	return runBenchmarkSeriesCommand(ctx, command, args, out)
}
func (s *Service) loadMarketDataFixture(path string) (marketdatacontracts.CandleSource, error) {
	module, err := marketmodule.Compose(marketmodule.Config{FixturePath: path, DB: s.db, Observations: observation.New(s.db, s.now), Ledger: s.Ledger, Now: s.now, ID: s.id})
	return module.Candles, err
}
func readBenchmarkSeriesStatus(ctx context.Context, db *sql.DB, id, version string) (*BenchmarkSeriesReceipt, error) {
	return newService(db, time.Now, randomID).Market.Imports.BenchmarkStatus(ctx, id, version)
}
func hasIssueCode(issues []APIError, code string) bool {
	return slices.ContainsFunc(issues, func(issue APIError) bool { return issue.Code == code })
}

const ownerPriceSource = "owner_declared"
const ownerDataPreviewResponseLimit = 6*maxBodyBytes + 16*1024
const twrCheckpointResponseLimit = 3 << 20

var errBenchmarkSeriesNotFound = marketdatacontracts.ErrBenchmarkNotFound

const noStrategySelection = "no_strategy"
const noStrategySelectionEvent = "no_event"
const capitalizedPaperSignalSchema = "paper-signal.v3"
const paperSignalSchema = "paper-signal.v2"
const canonicalPaperTimeLayout = "2006-01-02T15:04:05.000000000Z"

func tradingOwnerForTest(db *sql.DB, registry *observation.Registry) (tradingcontracts.Trading, error) {
	return tradingmodule.Compose(tradingmodule.Config{DB: db, Observations: registry, Clock: time.Now, ID: randomID, ExecutionOwner: "test_execution_owner", RunnerOwner: "test_runner_owner"})
}
func tradingRecoveryForTest(ctx context.Context, db *sql.DB, archival bool) (*tradingcontracts.RecoverySnapshot, error) {
	var schema int
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&schema); err != nil {
		return nil, err
	}
	registry := observation.New(db, time.Now)
	api, err := tradingOwnerForTest(db, registry)
	if err != nil {
		return nil, err
	}
	var result *tradingcontracts.RecoverySnapshot
	err = registry.VerifyBackup(ctx, func(ref storagecontracts.ObservationRef, _ time.Time) error {
		var err error
		result, err = api.VerifyBackup(ctx, ref, tradingcontracts.BackupQuery{Schema: schema, Archival: archival})
		return err
	})
	return result, err
}
func proveExecutionAuthorityRecovery(ctx context.Context, db *sql.DB) (string, int, error) {
	result, err := tradingRecoveryForTest(ctx, db, false)
	if err != nil {
		return "", 0, err
	}
	return result.Orders.ExecutionAuthoritySHA256, result.Orders.ExecutionAuthorityEvents, nil
}
func tradingMigrationForTest(ctx context.Context, db *sql.DB, schema int) (*tradingcontracts.MigrationSnapshot, error) {
	registry := observation.New(db, time.Now)
	api, err := tradingOwnerForTest(db, registry)
	if err != nil {
		return nil, err
	}
	tx, proof, err := registry.MigrationStarter().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var result *tradingcontracts.MigrationSnapshot
	err = proof.Verify(ctx, func(ref storagecontracts.ObservationRef, _ time.Time) error {
		var err error
		result, err = api.MigrationFacts(ctx, ref, tradingcontracts.MigrationQuery{Schema: schema})
		return err
	})
	return result, err
}

type testSchemaObject struct{ Type, Name, Table, SQL string }

func schemaObjectsForTest(ctx context.Context, db *sql.DB) ([]testSchemaObject, []testSchemaObject, []testSchemaObject, error) {
	rows, err := db.QueryContext(ctx, `SELECT type,name,tbl_name,COALESCE(sql,'') FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' AND tbl_name NOT IN ('paper_promotion_claims','paper_promotion_decisions') ORDER BY type,name`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	var objects, indexes, triggers []testSchemaObject
	for rows.Next() {
		var object testSchemaObject
		if err := rows.Scan(&object.Type, &object.Name, &object.Table, &object.SQL); err != nil {
			return nil, nil, nil, err
		}
		if object.Table == "strategy_selection_events" {
			if object.Type == "index" && object.SQL != "" {
				indexes = append(indexes, object)
			}
			continue
		}
		objects = append(objects, object)
		if object.Type == "trigger" && strings.Contains(object.SQL, "strategy_selection_events") {
			triggers = append(triggers, object)
		}
	}
	return objects, indexes, triggers, rows.Err()
}

type testForeignKey struct{ Child, Parent, From, To, OnUpdate, OnDelete, Match string }

func foreignKeysForTest(ctx context.Context, db *sql.DB) ([]testForeignKey, error) {
	rows, err := db.QueryContext(ctx, `SELECT m.name, f.[table], f.[from], f.[to], f.on_update, f.on_delete, f.match FROM sqlite_master AS m, pragma_foreign_key_list(m.name) AS f WHERE m.type='table' AND m.name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []testForeignKey{}
	for rows.Next() {
		var key testForeignKey
		if err := rows.Scan(&key.Child, &key.Parent, &key.From, &key.To, &key.OnUpdate, &key.OnDelete, &key.Match); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return foreignKeySortKey(keys[i]) < foreignKeySortKey(keys[j]) })
	return keys, rows.Err()
}
func foreignKeySortKey(key testForeignKey) string {
	return strings.Join([]string{key.Child, key.Parent, key.From, key.To, key.OnUpdate, key.OnDelete, key.Match}, "\x00")
}

type testMigration20Snapshot struct {
	AuthoritySHA   string
	AuthorityCount int
	Strategy       tradingcontracts.StrategyRecovery
	ForeignKeys    []testForeignKey
}

func captureMigration20Snapshot(ctx context.Context, db *sql.DB) (testMigration20Snapshot, error) {
	facts, err := tradingMigrationForTest(ctx, db, 19)
	if err != nil {
		return testMigration20Snapshot{}, err
	}
	keys, err := foreignKeysForTest(ctx, db)
	return testMigration20Snapshot{facts.AuthoritySHA, facts.AuthorityCount, facts.Strategy, keys}, err
}

type testMigration27Snapshot struct {
	Orders                                             tradingcontracts.OrderRecovery
	Strategy                                           tradingcontracts.StrategyRecovery
	SchemaObjects, SelectionIndexes, DependentTriggers []testSchemaObject
}

func captureMigration27Snapshot(ctx context.Context, db *sql.DB) (testMigration27Snapshot, error) {
	facts, err := tradingMigrationForTest(ctx, db, 26)
	if err != nil {
		return testMigration27Snapshot{}, err
	}
	objects, indexes, triggers, err := schemaObjectsForTest(ctx, db)
	return testMigration27Snapshot{facts.Orders, facts.Strategy, objects, indexes, triggers}, err
}
func sameStrategyRegistryProof(a, b tradingcontracts.StrategyRecovery) bool { return a == b }
func sameMigration27SchemaObjects(a, b []testSchemaObject) bool             { return reflect.DeepEqual(a, b) }
func sameMigration20Triggers(a, b []testSchemaObject) bool                  { return reflect.DeepEqual(a, b) }

func marketInputsForTest(ctx context.Context, db *sql.DB, at time.Time) (marketdatacontracts.ValuationInputs, error) {
	registry := observation.New(db, time.Now)
	ledger, err := ledgermodule.Compose(ledgermodule.Config{DB: db, Observations: registry, Now: time.Now, ID: randomID, PreviewTTL: func() time.Duration { return 15 * time.Minute }})
	if err != nil {
		return marketdatacontracts.ValuationInputs{}, err
	}
	market, err := marketmodule.Compose(marketmodule.Config{DB: db, Observations: registry, Ledger: ledger, Now: time.Now, ID: randomID})
	if err != nil {
		return marketdatacontracts.ValuationInputs{}, err
	}
	var result marketdatacontracts.ValuationInputs
	err = registry.ReadValuationAt(ctx, &at, func(ref storagecontracts.ObservationRef, _ time.Time) error {
		var err error
		result, err = market.Queries.ReadValuationInputs(ctx, ref, marketdatacontracts.ValuationQuery{Prices: true})
		return err
	})
	return result, err
}
func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func strategyCanonicalJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
}
func hasExactKeysForTest(value map[string]any, expected ...string) bool {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	slices.Sort(expected)
	return slices.Equal(keys, expected)
}
