import 'package:flutter/material.dart';
import 'package:omni_folio_client/app/client_dependencies.dart';
import 'package:omni_folio_client/app/data_page.dart';
import 'package:omni_folio_client/app/navigation.dart';
import 'package:omni_folio_client/core/presentation/signal_builder.dart';
import 'package:omni_folio_client/core/presentation/theme.dart';
import 'package:omni_folio_client/features/connections/presentation/connection_cards.dart';
import 'package:omni_folio_client/features/ledger/application/import_apply_controller.dart';
import 'package:omni_folio_client/features/ledger/presentation/activity_page.dart';
import 'package:omni_folio_client/features/market_data/application/owner_data_import_controller.dart';
import 'package:omni_folio_client/features/market_data/data/owner_data_file.dart';
import 'package:omni_folio_client/features/portfolio/application/portfolio_controller.dart';
import 'package:omni_folio_client/features/portfolio/presentation/portfolio_pages.dart';
import 'package:omni_folio_client/features/watchlist/application/watchlist_controller.dart';

class OmniFolioApp extends StatefulWidget {
  const OmniFolioApp({super.key, required this.dependencies});
  final ClientDependencies dependencies;

  @override
  State<OmniFolioApp> createState() => _OmniFolioAppState();
}

class _OmniFolioAppState extends State<OmniFolioApp> {
  late final WatchlistController _watchlist = WatchlistController(
    widget.dependencies.watchlist,
  );
  late final PortfolioController _portfolio = PortfolioController(
    widget.dependencies.portfolio,
    connections: widget.dependencies.connections,
  )..refresh();
  late final ImportApplyController _importApplies = ImportApplyController(
    widget.dependencies.ledger,
  );
  late final OwnerDataImportController _ownerImports =
      OwnerDataImportController(
        widget.dependencies.marketData,
        onStored: _portfolio.refreshAfterMarketDataImport,
        pickFile: selectOwnerDataFile,
      );
  var _tab = 0;

  @override
  void dispose() {
    _watchlist.dispose();
    _portfolio.dispose();
    _importApplies.dispose();
    _ownerImports.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'Kairos',
    theme: theme(Brightness.light),
    darkTheme: theme(Brightness.dark),
    themeMode: ThemeMode.system,
    home: SignalBuilder(
      signal: _portfolio,
      builder: (context, _) {
        const labels = ['홈', '보유', '내역', '연결'];
        final pages = [
          OverviewPage(
            controller: _portfolio,
            onImport: () => setState(() => _tab = 2),
            onHoldings: () => setState(() => _tab = 1),
            onAsset: (context, holding) => Navigator.of(context).push(
              MaterialPageRoute<void>(
                builder: (_) => assetDetailPage(widget.dependencies, holding),
              ),
            ),
            orderRisk: OverviewOrderRiskCard(
              orderLog: _portfolio.orderLog,
              retainedError: _portfolio.orderLogFailed,
              busy: _portfolio.orderLogBusy,
              onDetails: () => setState(() => _tab = 3),
            ),
            reconciliationCard: OverviewReconciliationCard(
              reconciliation: _portfolio.reconciliation,
              retainedError: _portfolio.reconciliationFailed,
              busy: _portfolio.reconciliationBusy,
              onDetails: () => setState(() => _tab = 3),
            ),
          ),
          HoldingsPage(
            controller: _portfolio,
            onAsset: (context, holding) => Navigator.of(context).push(
              MaterialPageRoute<void>(
                builder: (_) => assetDetailPage(widget.dependencies, holding),
              ),
            ),
          ),
          ActivityPage(
            api: widget.dependencies.ledger,
            applies: _importApplies,
          ),
          DataPage(
            dependencies: widget.dependencies,
            controller: _portfolio,
            ownerImports: _ownerImports,
          ),
        ];
        return LayoutBuilder(
          builder: (context, constraints) {
            final wide = constraints.maxWidth >= 900;
            final content = Center(
              child: ConstrainedBox(
                key: const Key('portfolio-content-frame'),
                constraints: const BoxConstraints(maxWidth: 1040),
                child: pages[_tab],
              ),
            );
            return Scaffold(
              appBar: AppBar(
                title: Text(wide ? 'Kairos' : labels[_tab]),
                actions: [
                  Builder(
                    builder: (context) => IconButton(
                      tooltip: '관심종목',
                      onPressed: () => Navigator.of(context).push(
                        MaterialPageRoute<void>(
                          builder: (_) =>
                              watchlistPage(widget.dependencies, _watchlist),
                        ),
                      ),
                      icon: const Icon(Icons.bookmark_outline),
                    ),
                  ),
                  IconButton(
                    tooltip: '데이터 새로고침',
                    onPressed: _portfolio.busy ? null : _portfolio.refresh,
                    icon: const Icon(Icons.refresh_rounded),
                  ),
                  const SizedBox(width: 8),
                ],
              ),
              body: SafeArea(
                // Keep the page mounted when only navigation placement changes.
                child: Row(
                  children: [
                    if (wide) ...[
                      NavigationRail(
                        selectedIndex: _tab,
                        labelType: NavigationRailLabelType.all,
                        groupAlignment: -0.8,
                        onDestinationSelected: (index) =>
                            setState(() => _tab = index),
                        destinations: const [
                          NavigationRailDestination(
                            icon: Icon(Icons.home_outlined),
                            selectedIcon: Icon(Icons.home_rounded),
                            label: Text('홈'),
                          ),
                          NavigationRailDestination(
                            icon: Icon(Icons.account_balance_wallet_outlined),
                            selectedIcon: Icon(Icons.account_balance_wallet),
                            label: Text('보유'),
                          ),
                          NavigationRailDestination(
                            icon: Icon(Icons.receipt_long_outlined),
                            selectedIcon: Icon(Icons.receipt_long),
                            label: Text('내역'),
                          ),
                          NavigationRailDestination(
                            icon: Icon(Icons.link_outlined),
                            selectedIcon: Icon(Icons.link),
                            label: Text('연결'),
                          ),
                        ],
                      ),
                      VerticalDivider(
                        width: 1,
                        color: Theme.of(context).dividerColor,
                      ),
                    ],
                    Expanded(child: content),
                  ],
                ),
              ),
              bottomNavigationBar: wide
                  ? null
                  : NavigationBar(
                      animationDuration: MediaQuery.disableAnimationsOf(context)
                          ? Duration.zero
                          : null,
                      selectedIndex: _tab,
                      onDestinationSelected: (index) =>
                          setState(() => _tab = index),
                      destinations: const [
                        NavigationDestination(
                          icon: Icon(Icons.home_outlined),
                          selectedIcon: Icon(Icons.home_rounded),
                          label: '홈',
                        ),
                        NavigationDestination(
                          icon: Icon(Icons.account_balance_wallet_outlined),
                          selectedIcon: Icon(Icons.account_balance_wallet),
                          label: '보유',
                        ),
                        NavigationDestination(
                          icon: Icon(Icons.receipt_long_outlined),
                          selectedIcon: Icon(Icons.receipt_long),
                          label: '내역',
                        ),
                        NavigationDestination(
                          icon: Icon(Icons.link_outlined),
                          selectedIcon: Icon(Icons.link),
                          label: '연결',
                        ),
                      ],
                    ),
            );
          },
        );
      },
    ),
  );
}
