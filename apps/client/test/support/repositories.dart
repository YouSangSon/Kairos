import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:omni_folio_client/app/app.dart';
import 'package:omni_folio_client/app/client_dependencies.dart';
import 'package:omni_folio_client/app/data_page.dart';
import 'package:omni_folio_client/app/navigation.dart';
import 'package:omni_folio_client/core/data/http_transport.dart';
import 'package:omni_folio_client/features/connections/domain/connections_repository.dart';
import 'package:omni_folio_client/features/connections/domain/models.dart';
import 'package:omni_folio_client/features/ledger/domain/ledger_repository.dart';
import 'package:omni_folio_client/features/ledger/domain/models.dart';
import 'package:omni_folio_client/features/market_data/application/owner_data_import_controller.dart';
import 'package:omni_folio_client/features/market_data/data/owner_data_file.dart';
import 'package:omni_folio_client/features/market_data/domain/market_data_repository.dart';
import 'package:omni_folio_client/features/market_data/domain/models.dart';
import 'package:omni_folio_client/features/market_data/presentation/asset_detail_page.dart';
import 'package:omni_folio_client/features/paper/domain/paper_monitor.dart';
import 'package:omni_folio_client/features/paper/domain/paper_promotion_report.dart';
import 'package:omni_folio_client/features/paper/domain/paper_repository.dart';
import 'package:omni_folio_client/features/portfolio/application/portfolio_controller.dart';
import 'package:omni_folio_client/features/portfolio/domain/models.dart';
import 'package:omni_folio_client/features/portfolio/domain/portfolio_repository.dart';
import 'package:omni_folio_client/features/research/domain/public_research.dart';
import 'package:omni_folio_client/features/research/domain/research_report.dart';
import 'package:omni_folio_client/features/research/domain/research_repository.dart';
import 'package:omni_folio_client/features/watchlist/application/watchlist_controller.dart';
import 'package:omni_folio_client/features/watchlist/domain/models.dart';
import 'package:omni_folio_client/features/watchlist/domain/watchlist_repository.dart';
import 'package:omni_folio_client/features/watchlist/presentation/watchlist_page.dart';

abstract interface class TestRepositories
    implements
        PortfolioRepository,
        LedgerRepository,
        MarketDataRepository,
        ConnectionsRepository,
        WatchlistRepository,
        ResearchRepository,
        PaperRepository {}

class RestTestRepositories implements TestRepositories {
  RestTestRepositories({http.Client? client, String? baseUrl})
    : dependencies = ClientDependencies.rest(
        HttpTransport(client: client, baseUrl: baseUrl),
      );
  final ClientDependencies dependencies;
  String get baseUrl => dependencies.connectionLabel;
  @override
  Future<WatchlistReport> watchlist() => dependencies.watchlist.watchlist();
  @override
  Future<WatchlistState> updateWatchlist(WatchlistChange change) =>
      dependencies.watchlist.updateWatchlist(change);
  @override
  Future<ServiceStatus> status() => dependencies.connections.status();
  @override
  Future<PortfolioSnapshot> snapshot() => dependencies.portfolio.snapshot();
  @override
  Future<CurrentPortfolioValuation> currentPortfolioValuation() =>
      dependencies.portfolio.currentPortfolioValuation();
  @override
  Future<PortfolioXirr> portfolioXirr() =>
      dependencies.portfolio.portfolioXirr();
  @override
  Future<PortfolioTwr> portfolioTwr() => dependencies.portfolio.portfolioTwr();
  @override
  Future<PortfolioTwrCheckpoints> portfolioTwrCheckpoints() =>
      dependencies.portfolio.portfolioTwrCheckpoints();
  @override
  Future<PortfolioBenchmarks> portfolioBenchmarks() =>
      dependencies.portfolio.portfolioBenchmarks();
  @override
  Future<HoldingValuation> holdingValuation() =>
      dependencies.portfolio.holdingValuation();
  @override
  Future<InstrumentLedger> instrumentLedger(
    String instrumentId,
    String currency,
  ) => dependencies.ledger.instrumentLedger(instrumentId, currency);
  @override
  Future<MarketCandles> candles(String symbol) =>
      dependencies.marketData.candles(symbol);
  @override
  Future<BrokerReconciliation?> latestBrokerReconciliation() =>
      dependencies.connections.latestBrokerReconciliation();
  @override
  Future<LocalOrderLog> localOrders() => dependencies.connections.localOrders();
  @override
  Future<PaperMonitor> paperMonitor() => dependencies.paper.paperMonitor();
  @override
  Future<PaperPromotionReport> paperPromotionReport() =>
      dependencies.paper.paperPromotionReport();
  @override
  Future<ResearchReport> researchReport() =>
      dependencies.research.researchReport();
  @override
  Future<PublicResearch> publicResearch(PublicResearchIdentity identity) =>
      dependencies.research.publicResearch(identity);
  @override
  Future<LedgerActivityPage> ledgerActivities({String? cursor}) =>
      dependencies.ledger.ledgerActivities(cursor: cursor);
  @override
  Future<ImportPreview> preview(String csv) => dependencies.ledger.preview(csv);
  @override
  Future<ApplyReceipt> apply(String previewId, String idempotencyKey) =>
      dependencies.ledger.apply(previewId, idempotencyKey);
  @override
  Future<OwnerDataPreview> previewOwnerData(String input) =>
      dependencies.marketData.previewOwnerData(input);
  @override
  Future<OwnerDataReceipt> applyOwnerData(OwnerDataPreview review) =>
      dependencies.marketData.applyOwnerData(review);
}

ClientDependencies testDependencies(TestRepositories api) => ClientDependencies(
  portfolio: api,
  ledger: api,
  marketData: api,
  connections: api,
  watchlist: api,
  research: api,
  paper: api,
  connectionLabel: api is RestTestRepositories ? api.baseUrl : '테스트 API',
);
OmniFolioApp testApp({Key? key, required TestRepositories api}) =>
    OmniFolioApp(key: key, dependencies: testDependencies(api));
AssetDetailPage testAssetDetailPage({
  Key? key,
  required TestRepositories api,
  required Holding holding,
}) => assetDetailPage(testDependencies(api), holding);
WatchlistPage testWatchlistPage({
  Key? key,
  required WatchlistController controller,
}) => watchlistPage(
  testDependencies(controller.api as TestRepositories),
  controller,
);
PortfolioController testPortfolioController(TestRepositories api) =>
    PortfolioController(api, connections: api);
DataPage testDataPage({
  Key? key,
  required TestRepositories api,
  required PortfolioController controller,
  OwnerDataImportController? ownerImports,
}) => DataPage(
  key: key,
  dependencies: testDependencies(api),
  controller: controller,
  ownerImports: ownerImports,
);
OwnerDataImportController testOwnerDataImportController(
  MarketDataRepository api, {
  required void Function() onStored,
}) => OwnerDataImportController(
  api,
  onStored: onStored,
  pickFile: selectOwnerDataFile,
);
