import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/core/domain/api_exception.dart';
import 'package:omni_folio_client/features/connections/data/codecs.dart';
import 'package:omni_folio_client/features/connections/domain/models.dart';
import 'package:omni_folio_client/features/ledger/data/codecs.dart';
import 'package:omni_folio_client/features/ledger/domain/models.dart';
import 'package:omni_folio_client/features/market_data/data/codecs.dart';
import 'package:omni_folio_client/features/market_data/domain/models.dart';
import 'package:omni_folio_client/features/paper/domain/paper_monitor.dart';
import 'package:omni_folio_client/features/paper/domain/paper_promotion_report.dart';
import 'package:omni_folio_client/features/portfolio/data/codecs.dart';
import 'package:omni_folio_client/features/portfolio/domain/models.dart';
import 'package:omni_folio_client/features/research/domain/public_research.dart';
import 'package:omni_folio_client/features/research/domain/research_report.dart';
import 'package:omni_folio_client/features/watchlist/data/codecs.dart';
import 'package:omni_folio_client/features/watchlist/domain/models.dart';
import 'repositories.dart';

Json fixture(String name) {
  final body = File('../../contracts/fixtures/$name').readAsStringSync();
  return jsonDecode(body) as Json;
}

class FakeApi implements TestRepositories {
  @override
  Future<WatchlistReport> watchlist() async => decodeWatchlistReport({
    'policy_version': 'watchlist_stored_prices_24h.v1',
    'max_observation_age_seconds': 86400,
    'observed_at': '2026-01-01T00:00:00Z',
    'watchlist': {'revision': 0, 'updated_at': null, 'entries': <Json>[]},
    'quotes': <Json>[],
  });
  @override
  Future<WatchlistState> updateWatchlist(WatchlistChange change) async =>
      throw UnimplementedError();

  @override
  Future<PortfolioTwrCheckpoints> portfolioTwrCheckpoints() =>
      throw UnsupportedError('Not used by this fixture.');

  @override
  Future<OwnerDataPreview> previewOwnerData(String input) async =>
      throw UnimplementedError();
  @override
  Future<OwnerDataReceipt> applyOwnerData(OwnerDataPreview review) async =>
      throw UnimplementedError();

  PortfolioBenchmarks? benchmarksValue;
  Completer<PortfolioBenchmarks>? benchmarksCompleter;
  int benchmarksCalls = 0;
  bool failBenchmarks = false;
  @override
  Future<PortfolioBenchmarks> portfolioBenchmarks() async {
    benchmarksCalls++;
    if (failBenchmarks) throw const ApiException('private-benchmark-sentinel');
    return benchmarksCompleter?.future ?? benchmarksValue!;
  }

  PortfolioTwr? twrValue;
  Completer<PortfolioTwr>? twrCompleter;
  int twrCalls = 0;
  bool failTwr = false;
  @override
  Future<PortfolioTwr> portfolioTwr() async {
    twrCalls++;
    if (failTwr) throw const ApiException('private-twr-sentinel');
    return twrCompleter?.future ?? twrValue!;
  }

  PortfolioXirr? xirrValue;
  Completer<PortfolioXirr>? xirrCompleter;
  int xirrCalls = 0;
  bool failXirr = false;

  @override
  Future<PortfolioXirr> portfolioXirr() async {
    xirrCalls += 1;
    if (failXirr) throw const ApiException('private-xirr-sentinel');
    return xirrCompleter?.future ?? xirrValue!;
  }

  FakeApi({
    required this.statusValue,
    required this.snapshotValue,
    required this.previewValue,
    required this.receiptValue,
    required this.candlesValue,
    required this.orderLogValue,
    required this.activityValue,
    this.currentValuationValue,
    this.reconciliationValue,
    this.fail = false,
    this.failSnapshot = false,
    this.failReconciliation = false,
    this.failLocalOrders = false,
    this.failActivities = false,
    this.failCurrentValuation = false,
    this.applyFailures = 0,
    this.failureMessage = '서버를 다시 확인하세요.',
  });

  final ServiceStatus statusValue;
  final PortfolioSnapshot snapshotValue;
  final ImportPreview previewValue;
  final ApplyReceipt receiptValue;
  MarketCandles candlesValue;
  LocalOrderLog orderLogValue;
  LedgerActivityPage activityValue;
  CurrentPortfolioValuation? currentValuationValue;
  Completer<CurrentPortfolioValuation>? currentValuationCompleter;
  int currentValuationCalls = 0;
  BrokerReconciliation? reconciliationValue;
  Completer<BrokerReconciliation?>? reconciliationCompleter;
  int reconciliationCalls = 0;
  Completer<LocalOrderLog>? localOrdersCompleter;
  int localOrderCalls = 0;
  Completer<LedgerActivityPage>? activityCompleter;
  int activityCalls = 0;
  Completer<MarketCandles>? candlesCompleter;
  int candleCalls = 0;
  Completer<ApplyReceipt>? applyCompleter;
  final List<String> applyKeys = [];
  final List<String> previewInputs = [];
  bool fail;
  bool failSnapshot;
  bool failReconciliation;
  bool failLocalOrders;
  bool failActivities;
  bool failCurrentValuation;
  int applyFailures;
  String failureMessage;

  @override
  Future<ApplyReceipt> apply(String previewId, String idempotencyKey) async {
    applyKeys.add(idempotencyKey);
    if (applyFailures > 0) {
      applyFailures -= 1;
      throw ApiException(failureMessage);
    }
    if (fail) throw ApiException(failureMessage);
    return applyCompleter?.future ?? receiptValue;
  }

  @override
  Future<ImportPreview> preview(String csv) async {
    previewInputs.add(csv);
    if (fail) throw ApiException(failureMessage);
    return previewValue;
  }

  @override
  Future<PortfolioSnapshot> snapshot() async {
    if (fail || failSnapshot) {
      throw ApiException(failureMessage);
    }
    return snapshotValue;
  }

  @override
  Future<CurrentPortfolioValuation> currentPortfolioValuation() async {
    currentValuationCalls += 1;
    if (fail || failCurrentValuation) {
      throw const ApiException('private-current-valuation-error');
    }
    return currentValuationCompleter?.future ?? currentValuationValue!;
  }

  @override
  Future<InstrumentLedger> instrumentLedger(
    String instrumentId,
    String currency,
  ) => throw UnsupportedError('Not used by this fixture.');

  @override
  Future<HoldingValuation> holdingValuation() =>
      throw UnsupportedError('Not used by this fixture.');

  @override
  Future<MarketCandles> candles(String symbol) async {
    candleCalls += 1;
    if (fail) throw ApiException(failureMessage);
    return candlesCompleter?.future ?? candlesValue;
  }

  @override
  Future<BrokerReconciliation?> latestBrokerReconciliation() async {
    reconciliationCalls += 1;
    if (fail || failReconciliation) {
      throw ApiException(failureMessage);
    }
    return reconciliationCompleter?.future ?? reconciliationValue;
  }

  @override
  Future<LocalOrderLog> localOrders() async {
    localOrderCalls += 1;
    if (fail || failLocalOrders) {
      throw const ApiException('kiwoom_account_secret');
    }
    return localOrdersCompleter?.future ?? orderLogValue;
  }

  @override
  Future<PaperMonitor> paperMonitor() =>
      throw UnsupportedError('Loaded only from the monitor detail page.');

  @override
  Future<PaperPromotionReport> paperPromotionReport() =>
      throw UnsupportedError('Loaded only from the paper comparison page.');

  @override
  Future<ResearchReport> researchReport() =>
      throw UnsupportedError('Loaded only from the research report page.');

  @override
  Future<PublicResearch> publicResearch(PublicResearchIdentity identity) =>
      throw UnsupportedError('Loaded only from the public research page.');

  @override
  Future<LedgerActivityPage> ledgerActivities({String? cursor}) async {
    activityCalls += 1;
    if (fail || failActivities) {
      throw const ApiException('account-main event-secret receipt-secret');
    }
    return activityCompleter?.future ?? activityValue;
  }

  @override
  Future<ServiceStatus> status() async {
    if (fail) throw ApiException(failureMessage);
    return statusValue;
  }
}

FakeApi goldenApi({
  bool neverVerified = false,
  ImportPreview? preview,
  PortfolioSnapshot? snapshot,
}) {
  final previewValue =
      preview ?? decodeImportPreview(fixture('golden-preview.json'));
  final snapshotValue =
      snapshot ?? decodePortfolioSnapshot(fixture('golden-snapshot.json'));
  final receipt = decodeApplyReceipt(fixture('golden-apply.json'));
  return FakeApi(
    statusValue: decodeServiceStatus({
      'live_enabled': false,
      'mode': 'local_import_only',
      'trust_state': neverVerified ? 'never_verified' : 'verified',
      'ledger_revision': 'rev_0000000003',
      'last_verified_at': neverVerified ? null : '2026-08-23T03:05:00Z',
      'issues': const [],
    }),
    snapshotValue: snapshotValue,
    currentValuationValue: currentPortfolioValuation(),
    previewValue: previewValue,
    receiptValue: receipt,
    candlesValue: marketCandles(),
    orderLogValue: decodeLocalOrderLog(localOrderLogJson(orders: const [])),
    activityValue: decodeLedgerActivityPage(ledgerActivitiesJson()),
  );
}

CurrentPortfolioValuation currentPortfolioValuation({
  String status = 'stale_sample',
  bool sample = true,
  String ledgerRevision = 'rev_0000000003',
  Object? cashValue = const {'currency': 'KRW', 'amount': '95530'},
  Object? holdingsValue = const {'currency': 'KRW', 'amount': '61650'},
  Object? totalValue = const {'currency': 'KRW', 'amount': '157180'},
  List<Json>? issues,
}) => decodeCurrentPortfolioValuation({
  'scope': 'whole_portfolio',
  'policy_version': 'current_base_portfolio_valuation_v1',
  'max_observation_age_seconds': 86400,
  'base_currency': 'KRW',
  'valuation_as_of': '2026-01-11T14:00:00Z',
  'ledger_revision': ledgerRevision,
  'ledger_as_of': '2026-01-10T10:00:00Z',
  'ledger_recorded_at': '2026-01-10T15:01:00Z',
  'status': status,
  'sample': sample,
  'cash_value': cashValue,
  'holdings_value': holdingsValue,
  'total_value': totalValue,
  'issues':
      issues ??
      const [
        {
          'code': 'sample_data',
          'message': 'portfolio valuation uses stored local sample data',
        },
      ],
});

MarketCandles marketCandles({
  String? instrumentId,
  String? currency,
  String barTimeBasis = 'unspecified',
  String state = 'stale',
  bool sample = true,
  String priceAdjustment = 'unspecified',
  String source = 'local_fixture',
  String? sourceAsOf = '2026-08-22T20:00:00Z',
  bool includeIssues = true,
  List<Json>? bars,
}) => decodeMarketCandles({
  'instrument_id': instrumentId,
  'currency': currency,
  'bar_time_basis': barTimeBasis,
  'symbol': 'AAPL',
  'venue': 'XNAS',
  'timezone': 'America/New_York',
  'interval': '1d',
  'price_adjustment': priceAdjustment,
  'source': source,
  'sample': sample,
  'state': state,
  'source_as_of': sourceAsOf,
  'fetched_at': '2026-08-24T03:00:00Z',
  'issues': !includeIssues
      ? const []
      : state == 'partial'
      ? [
          {'code': 'missing_session', 'message': '일부 세션이 없습니다.'},
        ]
      : source == 'local_fixture'
      ? [
          {
            'code': 'sample_data',
            'message': 'market data is a local sample and not live',
          },
        ]
      : const [],
  'bars': state == 'empty'
      ? const []
      : bars ??
            [
              {
                'at': '2026-08-21T20:00:00Z',
                'open': '100',
                'high': '110',
                'low': '90',
                'close': '105',
                'volume': '1200',
              },
              {
                'at': '2026-08-22T20:00:00Z',
                'open': '105',
                'high': '112',
                'low': '100',
                'close': '101',
                'volume': '900',
              },
            ],
});

Json brokerReconciliationJson() => {
  'provider': 'kiwoom',
  'environment': 'mock',
  'exchange': 'KRX',
  'freshness': 'unverified',
  'fetched_at': '2026-01-10T15:00:59Z',
  'recorded_at': '2026-01-10T15:01:00Z',
  'ledger_revision': 'rev_0000000002',
  'all_positions_match': false,
  'position_differences': const [
    {
      'symbol': '000660',
      'broker_quantity': '2',
      'ledger_quantity': '0',
      'difference': '2',
      'match': false,
    },
    {
      'symbol': '005930',
      'broker_quantity': '10',
      'ledger_quantity': '7',
      'difference': '3',
      'match': false,
    },
  ],
};

Json localOrderLogJson({List<Json>? orders}) => {
  'source': 'local_order_log',
  'broker_freshness': 'unverified',
  'orders':
      orders ??
      const [
        {
          'mode': 'synthetic',
          'symbol': '005930',
          'side': 'BUY',
          'order_type': 'LIMIT',
          'quantity': '2',
          'limit_price': '70000',
          'filled_quantity': '0',
          'currency': 'KRW',
          'status': 'SUBMIT_UNKNOWN',
          'pending_action': 'SUBMIT',
          'last_recorded_at': '2026-01-10T15:01:00Z',
        },
      ],
};

Json ledgerActivitiesJson({List<Json>? events, String? nextCursor}) => {
  'source': 'local_ledger',
  'broker_freshness': 'unverified',
  'ledger_revision': 'rev_0000000004',
  'recorded_at': '2026-01-10T15:01:00Z',
  'events':
      events ??
      const [
        {
          'type': 'FX_EXCHANGE',
          'occurred_at': '2026-01-03T00:00:00Z',
          'recorded_at': '2026-01-10T15:01:00Z',
          'symbol': null,
          'quantity': null,
          'price': null,
          'fee': null,
          'currency': 'USD',
          'amount': '-100',
          'counter_currency': 'KRW',
          'counter_amount': '137000',
          'is_correction': false,
        },
        {
          'type': 'BUY',
          'occurred_at': '2026-01-02T00:00:00Z',
          'recorded_at': '2026-01-10T15:01:00Z',
          'symbol': 'AAPL',
          'quantity': '2',
          'price': '10',
          'fee': '0',
          'currency': 'USD',
          'amount': '-20',
          'counter_currency': null,
          'counter_amount': null,
          'is_correction': false,
        },
      ],
  'next_cursor': nextCursor,
};

Future<void> pumpUi(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 300));
}
