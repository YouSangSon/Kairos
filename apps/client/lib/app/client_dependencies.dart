import 'package:omni_folio_client/core/data/http_transport.dart';
import 'package:omni_folio_client/features/connections/data/rest_connections_repository.dart';
import 'package:omni_folio_client/features/connections/domain/connections_repository.dart';
import 'package:omni_folio_client/features/ledger/data/rest_ledger_repository.dart';
import 'package:omni_folio_client/features/ledger/domain/ledger_repository.dart';
import 'package:omni_folio_client/features/market_data/data/rest_market_data_repository.dart';
import 'package:omni_folio_client/features/market_data/domain/market_data_repository.dart';
import 'package:omni_folio_client/features/paper/data/rest_paper_repository.dart';
import 'package:omni_folio_client/features/paper/domain/paper_repository.dart';
import 'package:omni_folio_client/features/portfolio/data/rest_portfolio_repository.dart';
import 'package:omni_folio_client/features/portfolio/domain/portfolio_repository.dart';
import 'package:omni_folio_client/features/research/data/rest_research_repository.dart';
import 'package:omni_folio_client/features/research/domain/research_repository.dart';
import 'package:omni_folio_client/features/watchlist/data/rest_watchlist_repository.dart';
import 'package:omni_folio_client/features/watchlist/domain/watchlist_repository.dart';

class ClientDependencies {
  const ClientDependencies({
    required this.portfolio,
    required this.ledger,
    required this.marketData,
    required this.connections,
    required this.watchlist,
    required this.research,
    required this.paper,
    required this.connectionLabel,
  });

  factory ClientDependencies.rest(HttpTransport transport) =>
      ClientDependencies(
        portfolio: RestPortfolioRepository(transport),
        ledger: RestLedgerRepository(transport),
        marketData: RestMarketDataRepository(transport),
        connections: RestConnectionsRepository(transport),
        watchlist: RestWatchlistRepository(transport),
        research: RestResearchRepository(transport),
        paper: RestPaperRepository(transport),
        connectionLabel: transport.baseUrl,
      );

  final PortfolioRepository portfolio;
  final LedgerRepository ledger;
  final MarketDataRepository marketData;
  final ConnectionsRepository connections;
  final WatchlistRepository watchlist;
  final ResearchRepository research;
  final PaperRepository paper;
  final String connectionLabel;
}
