import 'package:flutter/material.dart';
import 'package:omni_folio_client/app/client_dependencies.dart';
import 'package:omni_folio_client/features/ledger/presentation/instrument_ledger_page.dart';
import 'package:omni_folio_client/features/market_data/presentation/asset_detail_page.dart';
import 'package:omni_folio_client/features/portfolio/domain/models.dart';
import 'package:omni_folio_client/features/research/domain/public_research.dart';
import 'package:omni_folio_client/features/research/presentation/public_research_page.dart';
import 'package:omni_folio_client/features/watchlist/application/watchlist_controller.dart';
import 'package:omni_folio_client/features/watchlist/presentation/watchlist_page.dart';

AssetDetailPage assetDetailPage(
  ClientDependencies dependencies,
  Holding holding,
) => AssetDetailPage(
  api: dependencies.marketData,
  holding: holding,
  onLedger: (context) => Navigator.of(context).push(
    MaterialPageRoute<void>(
      builder: (_) =>
          InstrumentLedgerPage(api: dependencies.ledger, holding: holding),
    ),
  ),
  onResearch: (context) => selectPublicResearch(
    context,
    dependencies.research,
    symbol: holding.symbol,
  ),
);

WatchlistPage watchlistPage(
  ClientDependencies dependencies,
  WatchlistController controller,
) => WatchlistPage(
  controller: controller,
  onResearch: (context, entry) => Navigator.of(context).push(
    MaterialPageRoute<void>(
      builder: (_) => PublicResearchPage(
        api: dependencies.research,
        identity: PublicResearchIdentity.fromVenue(entry.venue, entry.symbol),
      ),
    ),
  ),
);
