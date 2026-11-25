import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/core/domain/api_exception.dart';
import 'package:omni_folio_client/features/portfolio/data/codecs.dart';
import 'package:omni_folio_client/features/portfolio/presentation/portfolio_pages.dart';
import 'package:omni_folio_client/features/portfolio/presentation/valuation_evidence_page.dart';
import 'support/fixtures.dart';
import 'support/repositories.dart';

const _cutoff = '2026-01-11T14:00:00Z';
const _ledger = {
  'valuation_as_of': _cutoff,
  'ledger_revision': 'rev_0000000003',
  'ledger_as_of': '2026-01-10T10:00:00Z',
  'ledger_recorded_at': '2026-01-11T13:00:00Z',
};
const _sampleIssue = {'code': 'sample_data', 'message': 'local sample data'};

const _old = '2026-01-09T09:00:00Z';

Json fxJson({
  String currency = 'USD',
  String rate = '1500.5',
  String id = 'fx-usd',
  String observed = '2026-01-11T09:00:00Z',
  String recorded = '2026-01-11T13:00:00Z',
}) => {
  'observation_id': id,
  'source': 'local_fixture',
  'base_currency': currency,
  'quote_currency': 'KRW',
  'rate': rate,
  'observed_at': observed,
  'fetched_at': observed,
  'recorded_at': recorded,
  'sample': true,
  'state': 'stale',
};

Json ownerFxJson({
  required String currency,
  required String rate,
  String observed = '2026-01-11T09:00:00Z',
  String? state,
}) => {
  ...fxJson(
    currency: currency,
    rate: rate,
    id: 'owner-$currency',
    observed: observed,
  ),
  'source': 'owner_declared',
  'source_version': 'fx-2026-01',
  'sample': false,
  'state': state ?? (observed == _old ? 'stale' : 'unknown'),
};

const _ownerIssue = {'code': 'owner_declared_fx', 'message': 'owner FX'};

Json cashLine(
  String currency,
  String amount,
  String status, {
  String? valued,
  Object? fx,
  Object? issue,
}) => {
  'currency': currency,
  'amount': amount,
  'status': status,
  'valued_amount': valued,
  'fx': fx,
  'issue': issue,
};

Json cashJson({
  String status = 'stale_sample',
  bool sample = true,
  Object? total = const {'currency': 'KRW', 'amount': '95015'},
  List<Json>? lines,
  List<Object>? issues,
}) => {
  'scope': 'cash_only',
  'policy_version': 'direct_fx_cash_v1',
  'max_observation_age_seconds': 86400,
  'base_currency': 'KRW',
  ..._ledger,
  'status': status,
  'sample': sample,
  'total': total,
  'lines':
      lines ??
      [
        cashLine('KRW', '50000', 'identity', valued: '50000'),
        cashLine('USD', '30', 'valued', valued: '45015', fx: fxJson()),
      ],
  'issues': issues ?? const [_sampleIssue],
};

Json holdingsJson() => {
  'scope': 'holdings_only',
  'policy_version': 'native_holding_valuation_v1',
  'max_observation_age_seconds': 86400,
  ..._ledger,
  'status': 'stale_sample',
  'sample': true,
  'totals': const [
    {
      'currency': 'USD',
      'cost_basis': '31',
      'market_value': '45',
      'unrealized_pnl': '14',
    },
  ],
  'lines': const [
    {
      'symbol': 'AAPL',
      'quantity': '3',
      'cost_basis': '31',
      'currency': 'USD',
      'status': 'valued',
      'price': {
        'source': 'local_fixture',
        'venue': 'XNAS',
        'currency': 'USD',
        'price': '15',
        'price_adjustment': 'unspecified',
        'observed_at': '2026-01-10T14:00:00Z',
        'fetched_at': '2026-01-10T14:00:01Z',
        'recorded_at': '2026-01-11T13:00:00Z',
        'sample': true,
        'state': 'stale',
      },
      'market_value': '45',
      'unrealized_pnl': '14',
      'issue': null,
    },
  ],
  'issues': const [_sampleIssue],
};

Json currentJson({bool unavailable = false}) => {
  'scope': 'whole_portfolio',
  'policy_version': 'current_base_portfolio_valuation_v1',
  'max_observation_age_seconds': 86400,
  'base_currency': 'KRW',
  ..._ledger,
  'status': unavailable ? 'unavailable' : 'stale_sample',
  'sample': true,
  'cash_value': unavailable
      ? null
      : const {'currency': 'KRW', 'amount': '95015'},
  'holdings_value': unavailable
      ? null
      : const {'currency': 'KRW', 'amount': '67522.5'},
  'total_value': unavailable
      ? null
      : const {'currency': 'KRW', 'amount': '162537.5'},
  'issues': unavailable
      ? const [
          {'code': 'missing_direct_fx', 'message': 'missing', 'field': 'EUR'},
          {'code': 'stale_direct_fx', 'message': 'stale', 'field': 'USD'},
        ]
      : const [_sampleIssue],
};

Json evidenceJson({
  Json? cash,
  Json? holdings,
  Json? current,
  List<Json>? fx,
}) => {
  'schema_version': 'portfolio-valuation-evidence.v1',
  'observation_cutoff': _cutoff,
  'source_availability_at_cutoff': 'unverified',
  'cash': cash ?? cashJson(),
  'holdings': holdings ?? holdingsJson(),
  'current': current ?? currentJson(),
  'conversion_fx': fx ?? [fxJson()],
};

const _missingEur = {
  'code': 'missing_direct_fx',
  'message': 'missing',
  'field': 'EUR',
};
const _staleUsd = {
  'code': 'stale_direct_fx',
  'message': 'stale',
  'field': 'USD',
};

Json unavailableEvidenceJson() => evidenceJson(
  cash: cashJson(
    status: 'unavailable',
    total: null,
    lines: [
      cashLine('KRW', '50000', 'identity', valued: '50000'),
      cashLine('EUR', '10', 'missing', issue: _missingEur),
      cashLine('USD', '30', 'stale', fx: _staleUsdFx, issue: _staleUsd),
    ],
    issues: const [_missingEur, _staleUsd],
  ),
  current: currentJson(unavailable: true),
  fx: [_staleUsdFx],
);

final _staleUsdFx = fxJson(id: 'fx-usd-old', observed: _old);

Json _current(Map<String, Object?> changes) => {...currentJson(), ...changes};

/// Server-shaped responses beyond the default sample: owner FX (policy v2,
/// issue order owner then sample), an over-age owner rate, zero and negative
/// cash, empty cash, and USD conversion kept for USD holdings with zero USD cash.
final serverShapedEvidence = <String, Json>{
  'owner and fixture sample': evidenceJson(
    cash: {
      ...cashJson(
        total: const {'currency': 'KRW', 'amount': '101500'},
        lines: [
          cashLine('KRW', '50000', 'identity', valued: '50000'),
          cashLine(
            'USD',
            '30',
            'valued',
            valued: '42000',
            fx: ownerFxJson(currency: 'USD', rate: '1400'),
          ),
          cashLine(
            'JPY',
            '1000',
            'valued',
            valued: '9500',
            fx: fxJson(currency: 'JPY', rate: '9.5', id: 'fx-jpy'),
          ),
        ],
        issues: const [_ownerIssue, _sampleIssue],
      ),
      'policy_version': 'direct_fx_cash_v2',
    },
    current: _current({
      'policy_version': 'current_base_portfolio_valuation_v2',
      'cash_value': const {'currency': 'KRW', 'amount': '101500'},
      'holdings_value': const {'currency': 'KRW', 'amount': '63000'},
      'total_value': const {'currency': 'KRW', 'amount': '164500'},
      'issues': const [_ownerIssue, _sampleIssue],
    }),
    fx: [
      ownerFxJson(currency: 'USD', rate: '1400'),
      fxJson(currency: 'JPY', rate: '9.5', id: 'fx-jpy'),
    ],
  ),
  'stale owner rate with zero and negative cash': evidenceJson(
    cash: {
      ...cashJson(
        status: 'unavailable',
        sample: false,
        total: null,
        lines: [
          cashLine('KRW', '-1000', 'identity', valued: '-1000'),
          cashLine('USD', '0', 'zero', valued: '0'),
          cashLine(
            'EUR',
            '10',
            'stale',
            fx: ownerFxJson(currency: 'EUR', rate: '1600', observed: _old),
            issue: const {
              'code': 'stale_direct_fx',
              'message': 'stale',
              'field': 'EUR',
            },
          ),
        ],
        issues: const [
          {'code': 'stale_direct_fx', 'message': 'stale', 'field': 'EUR'},
          _ownerIssue,
        ],
      ),
      'policy_version': 'direct_fx_cash_v2',
    },
    current: _current({
      ...currentJson(unavailable: true),
      'policy_version': 'current_base_portfolio_valuation_v2',
      'issues': const [
        {'code': 'stale_direct_fx', 'message': 'stale', 'field': 'EUR'},
        _ownerIssue,
      ],
    }),
    fx: [
      ownerFxJson(currency: 'EUR', rate: '1600', observed: _old),
      fxJson(),
    ],
  ),
  'empty cash': evidenceJson(
    cash: cashJson(
      status: 'empty',
      sample: false,
      total: null,
      lines: const [],
      issues: const [],
    ),
    current: _current({
      'cash_value': const {'currency': 'KRW', 'amount': '0'},
      'total_value': const {'currency': 'KRW', 'amount': '67522.5'},
    }),
  ),
};

FakeApi evidenceApi(List<Object> responses) {
  final api = goldenApi();
  api.valuationEvidenceHandler = () async {
    final next = responses.removeAt(0);
    if (next is Exception) throw next;
    return decodePortfolioValuationEvidence(next as Json);
  };
  return api;
}

Future<void> pumpEvidence(WidgetTester tester, FakeApi api) async {
  await tester.pumpWidget(MaterialApp(home: ValuationEvidencePage(api: api)));
  await tester.pumpAndSettle();
}

Json _with(Json value, String key, Object? replacement) => {
  ...value,
  key: replacement,
};

void main() {
  test(
    'valuation evidence GET has no query and keeps one observation',
    () async {
      late http.Request request;
      final api = RestTestRepositories(
        baseUrl: 'http://example.test/',
        client: MockClient((value) async {
          request = value;
          return http.Response(jsonEncode(evidenceJson()), 200);
        }),
      );

      final evidence = await api.valuationEvidence();

      expect(request.method, 'GET');
      expect(request.url.path, '/v1/portfolio/valuation-evidence');
      expect(request.url.hasQuery, isFalse);
      expect(evidence.observationCutoff, _cutoff);
      expect(evidence.ledgerRevision, 'rev_0000000003');
      expect(evidence.cash.total?.amount, '95015');
      expect(evidence.cash.lines.last.valuedAmount, '45015');
      expect(evidence.cash.lines.last.fx?.rate, '1500.5');
      expect(evidence.holdings.lines.single.marketValue, '45');
      expect(evidence.current.totalValue?.amount, '162537.5');
      expect(evidence.conversionFx.single.baseCurrency, 'USD');
    },
  );

  test('valuation evidence parser accepts missing and stale direct FX', () {
    final evidence = decodePortfolioValuationEvidence(
      unavailableEvidenceJson(),
    );

    expect(evidence.cash.total, isNull);
    expect(evidence.cash.lines.map((line) => line.status), [
      'identity',
      'missing',
      'stale',
    ]);
    expect(evidence.current.totalValue, isNull);
  });

  test('valuation evidence parser accepts server-shaped variants', () {
    for (final MapEntry(:key, :value) in serverShapedEvidence.entries) {
      expect(
        () => decodePortfolioValuationEvidence(value),
        returnsNormally,
        reason: key,
      );
    }
    final owner = decodePortfolioValuationEvidence(
      serverShapedEvidence['owner and fixture sample']!,
    );
    expect(owner.cash.lines[1].fx?.sourceVersion, 'fx-2026-01');
    expect(owner.cash.lines[1].fx?.state, 'unknown');
  });

  test('valuation evidence parser rejects observation and math drift', () {
    final valid = evidenceJson();
    final cash = cashJson();
    final lines = (cash['lines'] as List).cast<Json>();
    final usd = lines.last;
    final mutations = <String, Json>{
      'extra key': {...valid, 'private_account_id': 'secret'},
      'schema': _with(
        valid,
        'schema_version',
        'portfolio-valuation-evidence.v2',
      ),
      'availability': _with(valid, 'source_availability_at_cutoff', 'verified'),
      'cutoff': _with(valid, 'observation_cutoff', '2026-01-11T15:00:00Z'),
      'holdings revision': evidenceJson(
        holdings: _with(holdingsJson(), 'ledger_revision', 'rev_0000000004'),
      ),
      'cash recorded at': evidenceJson(
        cash: _with(cash, 'ledger_recorded_at', '2026-01-11T12:00:00Z'),
      ),
      'cash base': evidenceJson(cash: _with(cash, 'base_currency', 'USD')),
      'cash scope': evidenceJson(cash: _with(cash, 'scope', 'whole_portfolio')),
      'cash extra key': evidenceJson(cash: {...cash, 'account': 'secret'}),
      'cash total': evidenceJson(
        cash: _with(cash, 'total', const {
          'currency': 'KRW',
          'amount': '95016',
        }),
      ),
      'cash total currency': evidenceJson(
        cash: _with(cash, 'total', const {
          'currency': 'USD',
          'amount': '95015',
        }),
      ),
      'valued amount with matching total': evidenceJson(
        cash: cashJson(
          total: const {'currency': 'KRW', 'amount': '95000'},
          lines: [lines.first, _with(usd, 'valued_amount', '45000')],
        ),
      ),
      'valued without fx': evidenceJson(
        cash: _with(cash, 'lines', [lines.first, _with(usd, 'fx', null)]),
      ),
      'fx pair': evidenceJson(
        cash: _with(cash, 'lines', [
          lines.first,
          _with(usd, 'fx', fxJson(currency: 'EUR')),
        ]),
      ),
      'identity non-KRW': evidenceJson(
        cash: _with(cash, 'lines', [
          cashLine('USD', '30', 'identity', valued: '30'),
        ]),
      ),
      'missing with value': evidenceJson(
        cash: cashJson(
          status: 'unavailable',
          total: null,
          lines: [
            cashLine('EUR', '10', 'missing', valued: '0', issue: _missingEur),
          ],
          issues: const [_missingEur],
        ),
        current: currentJson(unavailable: true),
      ),
      'stale without issue': evidenceJson(
        cash: cashJson(
          status: 'unavailable',
          total: null,
          lines: [cashLine('USD', '30', 'stale', fx: fxJson())],
          issues: const [],
        ),
        current: currentJson(unavailable: true),
      ),
      'unavailable with total': evidenceJson(
        cash: cashJson(
          status: 'unavailable',
          lines: [cashLine('EUR', '10', 'missing', issue: _missingEur)],
          issues: const [_missingEur],
        ),
      ),
      'sample without sample status': evidenceJson(
        cash: cashJson(status: 'complete', issues: const []),
      ),
      'fixture fx version': evidenceJson(
        fx: [
          {...fxJson(), 'source_version': 'v1'},
        ],
      ),
      'conversion quote': evidenceJson(
        fx: [_with(fxJson(), 'quote_currency', 'USD')],
      ),
      'duplicate conversion fx': evidenceJson(fx: [fxJson(), fxJson()]),
      'over-age valued fx': evidenceJson(
        cash: cashJson(
          lines: [
            lines.first,
            _with(usd, 'fx', fxJson(observed: '2026-01-01T09:00:00Z')),
          ],
        ),
        fx: [fxJson(observed: '2026-01-01T09:00:00Z')],
      ),
      'fresh stale fx': evidenceJson(
        cash: cashJson(
          status: 'unavailable',
          total: null,
          lines: [
            cashLine('USD', '30', 'stale', fx: fxJson(), issue: _staleUsd),
          ],
          issues: const [_staleUsd],
        ),
        current: currentJson(unavailable: true),
      ),
      'fx recorded after cutoff': evidenceJson(
        fx: [
          fxJson(),
          fxJson(id: 'late', recorded: '2026-01-11T15:00:00Z'),
        ],
      ),
      'fx fetched before observed': evidenceJson(
        fx: [
          fxJson(),
          _with(fxJson(id: 'early'), 'fetched_at', '2026-01-11T08:00:00Z'),
        ],
      ),
      'old owner fx marked unknown': evidenceJson(
        fx: [
          fxJson(),
          ownerFxJson(
            currency: 'EUR',
            rate: '1600',
            observed: _old,
            state: 'unknown',
          ),
        ],
      ),
      'cash total differs from current cash value': evidenceJson(
        current: _current({
          'cash_value': const {'currency': 'KRW', 'amount': '95016'},
          'total_value': const {'currency': 'KRW', 'amount': '162538.5'},
        }),
      ),
      'unavailable cash with available current': evidenceJson(
        cash: cashJson(
          status: 'unavailable',
          total: null,
          lines: [cashLine('EUR', '10', 'missing', issue: _missingEur)],
          issues: const [_missingEur],
        ),
      ),
      'cash fx missing from conversion': evidenceJson(fx: const []),
      'complete without total': evidenceJson(
        cash: cashJson(
          status: 'complete',
          sample: false,
          total: null,
          lines: [lines.first],
          issues: const [],
        ),
      ),
    };
    for (final MapEntry(:key, :value) in mutations.entries) {
      expect(
        () => decodePortfolioValuationEvidence(value),
        throwsFormatException,
        reason: key,
      );
    }
  });

  testWidgets('Overview opens the same-observation valuation evidence', (
    tester,
  ) async {
    final api = evidenceApi([evidenceJson()]);
    await tester.pumpWidget(testApp(api: api));
    await tester.pumpAndSettle();

    final open = find.text('같은 시점의 평가 근거 보기');
    await tester.scrollUntilVisible(
      open.hitTestable(),
      200,
      scrollable: find
          .descendant(
            of: find.byType(OverviewPage),
            matching: find.byType(Scrollable),
          )
          .first,
    );
    await tester.pump();
    await tester.tap(open);
    await tester.pumpAndSettle();

    expect(api.valuationEvidenceCalls, 1);
    expect(find.text('평가 근거'), findsOneWidget);
    expect(find.text('관측 기준 2026-01-11 14:00:00 UTC'), findsOneWidget);
    expect(find.textContaining('rev_0000000003'), findsOneWidget);
    expect(find.textContaining('공개·이용 가능했는지는 확인되지 않았습니다'), findsOneWidget);
    expect(find.text('예시 자료 기준 총 자산'), findsOneWidget);
    expect(find.text('KRW 162,537.5'), findsOneWidget);
  });

  testWidgets('cash lines keep native amounts, FX and missing reasons', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(800, 4000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await pumpEvidence(tester, evidenceApi([unavailableEvidenceJson()]));

    expect(find.text('전체 평가를 계산할 수 없음'), findsOneWidget);
    expect(find.textContaining('부분 합계는 표시하지 않습니다'), findsOneWidget);
    expect(find.text('총 자산'), findsNothing);
    expect(find.text('KRW 50000'), findsOneWidget);
    expect(find.text('KRW 그대로 합산'), findsOneWidget);
    expect(find.text('EUR 10'), findsOneWidget);
    expect(find.text('적격 환율 없음 · KRW 환산에서 제외'), findsOneWidget);
    expect(find.text('USD 30'), findsOneWidget);
    expect(find.text('환율이 24시간 정책을 초과 · KRW 환산에서 제외'), findsOneWidget);
    expect(find.text('USD/KRW 1500.5'), findsNWidgets(2));
    expect(find.text('예시 환율 포함 · 실제 계좌 평가가 아닙니다.'), findsOneWidget);
    expect(find.text('샘플 저장 가격 · 실시간/현재 시세 아님'), findsOneWidget);
    expect(find.text('AAPL'), findsOneWidget);
  });

  testWidgets('refresh keeps the last evidence and redacts thrown details', (
    tester,
  ) async {
    final api = evidenceApi([
      const ApiException('private-first-error'),
      evidenceJson(),
      const ApiException('private-refresh-error'),
    ]);
    await pumpEvidence(tester, api);

    expect(find.text('평가 근거를 불러오지 못했습니다'), findsOneWidget);
    expect(find.textContaining('private-first-error'), findsNothing);
    await tester.tap(find.text('다시 시도'));
    await tester.pumpAndSettle();
    expect(find.text('관측 기준 2026-01-11 14:00:00 UTC'), findsOneWidget);

    await tester.tap(find.byTooltip('평가 근거 새로고침'));
    await tester.pumpAndSettle();

    expect(api.valuationEvidenceCalls, 3);
    expect(find.text('새로고침하지 못했습니다. 이전 근거를 유지합니다.'), findsOneWidget);
    expect(find.textContaining('private-refresh-error'), findsNothing);
    expect(find.text('관측 기준 2026-01-11 14:00:00 UTC'), findsOneWidget);
  });

  testWidgets('320px at 200 percent keeps evidence readable and labelled', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 760);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final semantics = tester.ensureSemantics();

    await tester.pumpWidget(
      MaterialApp(
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(
            context,
          ).copyWith(textScaler: const TextScaler.linear(2)),
          child: child!,
        ),
        home: ValuationEvidencePage(api: evidenceApi([evidenceJson()])),
      ),
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.text('USD 30'),
      400,
      scrollable: find.descendant(
        of: find.byKey(const Key('valuation-evidence-list')),
        matching: find.byType(Scrollable),
      ),
    );
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(find.byTooltip('평가 근거 새로고침'), findsOneWidget);
    expect(
      find.semantics.byLabel(
        RegExp(
          r'USD 30.*KRW 45015.*USD/KRW 1500\.5.*예시 자료.*관측 2026-01-11 09:00:00 UTC',
        ),
      ),
      findsOneWidget,
    );
    semantics.dispose();
  });
}
