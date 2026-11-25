import 'package:flutter/material.dart';
import 'package:omni_folio_client/core/presentation/components.dart';
import 'package:omni_folio_client/features/portfolio/domain/models.dart';
import 'package:omni_folio_client/features/portfolio/domain/portfolio_repository.dart';
import 'package:omni_folio_client/features/portfolio/presentation/holding_valuation_page.dart';

class ValuationEvidencePage extends StatefulWidget {
  const ValuationEvidencePage({super.key, required this.api});

  final PortfolioRepository api;

  @override
  State<ValuationEvidencePage> createState() => _ValuationEvidencePageState();
}

class _ValuationEvidencePageState extends State<ValuationEvidencePage> {
  PortfolioValuationEvidence? _evidence;
  String? _error;
  var _busy = false;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final evidence = await widget.api.valuationEvidence();
      if (mounted) setState(() => _evidence = evidence);
    } catch (_) {
      if (mounted) {
        setState(() {
          _error = _evidence == null
              ? '평가 근거를 불러오지 못했습니다'
              : '새로고침하지 못했습니다. 이전 근거를 유지합니다.';
        });
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('평가 근거', maxLines: 1, overflow: TextOverflow.ellipsis),
      actions: [
        IconButton(
          tooltip: '평가 근거 새로고침',
          onPressed: _busy ? null : _refresh,
          icon: const Icon(Icons.refresh_rounded),
        ),
        const SizedBox(width: 8),
      ],
    ),
    body: SafeArea(child: _body(context)),
  );

  Widget _body(BuildContext context) {
    final evidence = _evidence;
    if (evidence == null && _busy) {
      return Center(
        child: Semantics(
          liveRegion: true,
          label: '평가 근거 불러오는 중',
          child: const CircularProgressIndicator(),
        ),
      );
    }
    if (evidence == null) {
      return ValuationMessage(
        icon: Icons.cloud_off_outlined,
        title: _error ?? '평가 근거를 불러오지 못했습니다',
        body: '민감한 오류 정보는 표시하지 않습니다. 서버를 확인한 뒤 다시 시도하세요.',
        action: _busy ? null : _refresh,
      );
    }
    final colors = Theme.of(context).colorScheme;
    final holdings = evidence.holdings;
    final items = <Widget>[
      if (_busy) const LinearProgressIndicator(),
      if (_error != null)
        ValuationNotice(
          icon: Icons.sync_problem_outlined,
          text: _error!,
          color: colors.error,
        ),
      _ObservationCard(evidence: evidence),
      ValuationNotice(
        icon: Icons.info_outline,
        text:
            '외부 자료가 언제 공개·이용 가능했는지는 확인되지 않았습니다. '
            '아래 시각은 저장된 관측·조회·기록 시각입니다.',
        color: colors.tertiary,
      ),
      const _SectionTitle('KRW 전체 평가'),
      _CurrentTotalCard(current: evidence.current),
      const _SectionTitle('현금 · 원통화'),
      _CashSummary(cash: evidence.cash),
      for (final line in evidence.cash.lines) _CashLineCard(line: line),
      const _SectionTitle('보유 · 원통화'),
      if (holdings.lines.isNotEmpty)
        ValuationNotice(
          icon: Icons.info_outline,
          text: holdings.sample
              ? '샘플 저장 가격 · 실시간/현재 시세 아님'
              : '저장된 평가 증거 · 실시간/현재 시세 아님',
          color: colors.tertiary,
        ),
      if (holdings.ownerDeclaredPrice)
        ValuationNotice(
          icon: Icons.info_outline,
          text: '직접 입력 가격 사용 · 시세 제공자가 검증한 값이 아닙니다.',
          color: colors.tertiary,
        ),
      if (holdings.status == 'empty') const Text('평가할 보유 종목이 없습니다.'),
      if (holdings.status == 'unavailable')
        ValuationNotice(
          icon: Icons.warning_amber_rounded,
          text: '일부 종목을 평가할 수 없어 통화별 합계를 표시하지 않습니다.',
          color: colors.error,
        ),
      for (final total in holdings.totals)
        HoldingValuationTotalCard(total: total),
      for (final line in holdings.lines) HoldingValuationLineCard(line: line),
      const _SectionTitle('KRW 환산에 쓴 환율'),
      if (evidence.conversionFx.isEmpty) const Text('KRW 환산에 쓴 직접 환율이 없습니다.'),
      for (final fx in evidence.conversionFx) _FxCard(fx: fx),
    ];
    return ListView.builder(
      key: const Key('valuation-evidence-list'),
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
      itemCount: items.length,
      itemBuilder: (context, index) => Padding(
        padding: EdgeInsets.only(top: index == 0 ? 0 : 12),
        child: items[index],
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle(this.text);

  final String text;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: 8),
    child: Semantics(
      header: true,
      child: Text(text, style: Theme.of(context).textTheme.titleMedium),
    ),
  );
}

class _ObservationCard extends StatelessWidget {
  const _ObservationCard({required this.evidence});

  final PortfolioValuationEvidence evidence;

  @override
  Widget build(BuildContext context) {
    final facts = [
      '관측 기준 ${readableValuationTime(evidence.observationCutoff)}',
      '원장 ${evidence.ledgerRevision} · 기록 '
          '${readableValuationTime(evidence.current.ledgerRecordedAt)}',
      '현금·보유·KRW 합계를 한 번의 읽기로 계산했습니다.',
    ];
    return Semantics(
      container: true,
      label: ['같은 관측의 평가 근거', ...facts].join(', '),
      child: ExcludeSemantics(
        child: Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '같은 관측의 평가 근거',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                for (final fact in facts) ...[
                  const SizedBox(height: 6),
                  Text(fact),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _CurrentTotalCard extends StatelessWidget {
  const _CurrentTotalCard({required this.current});

  final CurrentPortfolioValuation current;

  @override
  Widget build(BuildContext context) {
    final total = current.totalValue;
    final notices = [
      if (current.sample) '예시 자료 포함 · 실제 계좌 평가가 아닙니다.',
      if (current.ownerDeclaredFx) '직접 입력 환율 사용 · 시세 제공자가 검증한 값이 아닙니다.',
      if (current.ownerDeclaredPrice) '직접 입력 가격 사용 · 시세 제공자가 검증한 값이 아닙니다.',
    ];
    final lines = total == null
        ? [
            current.status == 'empty' ? '평가할 자산 없음' : '전체 평가를 계산할 수 없음',
            if (current.status != 'empty')
              '필요한 저장 가격 또는 직접 환율 ${current.missingInputCount}건을 확인하세요. '
                  '부분 합계는 표시하지 않습니다.',
          ]
        : [
            current.sample ? '예시 자료 기준 총 자산' : '총 자산',
            formattedMoney(total),
            '현금 ${formattedMoney(current.cashValue!)}',
            '보유 자산 ${formattedMoney(current.holdingsValue!)}',
          ];
    return Semantics(
      container: true,
      label: [...lines, ...notices].join(', '),
      child: ExcludeSemantics(
        child: Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                for (final (index, line) in lines.indexed)
                  Padding(
                    padding: EdgeInsets.only(top: index == 0 ? 0 : 4),
                    child: Text(
                      line,
                      style: total != null && index == 1
                          ? Theme.of(context).textTheme.headlineSmall?.copyWith(
                              fontFeatures: const [
                                FontFeature.tabularFigures(),
                              ],
                              fontWeight: FontWeight.w800,
                            )
                          : index == 0
                          ? Theme.of(context).textTheme.titleMedium
                          : null,
                    ),
                  ),
                for (final notice in notices) ...[
                  const SizedBox(height: 8),
                  Text(
                    notice,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: warningColor(context),
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _CashSummary extends StatelessWidget {
  const _CashSummary({required this.cash});

  final CashValuation cash;

  @override
  Widget build(BuildContext context) {
    final notices = [
      if (cash.sample) '예시 환율 포함 · 실제 계좌 평가가 아닙니다.',
      if (cash.issues.any((issue) => issue.code == 'owner_declared_fx'))
        '직접 입력 환율 사용 · 시세 제공자가 검증한 값이 아닙니다.',
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(switch (cash.total) {
          final total? => '현금 KRW 합계 ${formattedMoney(total)}',
          null when cash.status == 'empty' => '현금 잔액이 없습니다.',
          null => '일부 통화를 환산할 수 없어 현금 KRW 합계를 표시하지 않습니다.',
        }),
        for (final notice in notices) ...[
          const SizedBox(height: 4),
          Text(
            notice,
            style: Theme.of(
              context,
            ).textTheme.bodySmall?.copyWith(color: warningColor(context)),
          ),
        ],
      ],
    );
  }
}

class _CashLineCard extends StatelessWidget {
  const _CashLineCard({required this.line});

  final CashValuationLine line;

  @override
  Widget build(BuildContext context) {
    final title = '${line.currency} ${line.amount}';
    final status = switch (line.status) {
      'identity' => 'KRW 그대로 합산',
      'zero' => '잔액 0 · 환산 없음',
      'valued' => 'KRW ${line.valuedAmount} 환산',
      'missing' => '적격 환율 없음 · KRW 환산에서 제외',
      _ => '환율이 24시간 정책을 초과 · KRW 환산에서 제외',
    };
    final excluded = line.valuedAmount == null;
    final fx = line.fx;
    return Semantics(
      container: true,
      label: [title, status, if (fx != null) ..._fxFacts(fx)].join(', '),
      child: ExcludeSemantics(
        child: Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: 6),
                Text(
                  status,
                  style: excluded
                      ? TextStyle(color: Theme.of(context).colorScheme.error)
                      : null,
                ),
                if (fx != null) ...[
                  const SizedBox(height: 12),
                  const Divider(height: 1),
                  const SizedBox(height: 12),
                  ..._fxTexts(context, fx),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _FxCard extends StatelessWidget {
  const _FxCard({required this.fx});

  final ValuationFx fx;

  @override
  Widget build(BuildContext context) => Semantics(
    container: true,
    label: _fxFacts(fx).join(', '),
    child: ExcludeSemantics(
      child: Card(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: _fxTexts(context, fx),
          ),
        ),
      ),
    ),
  );
}

List<String> _fxFacts(ValuationFx fx) => [
  '${fx.baseCurrency}/${fx.quoteCurrency} ${fx.rate}',
  fx.source == 'owner_declared'
      ? [
          '직접 입력 환율',
          '입력 버전 ${fx.sourceVersion}',
          if (fx.sample) '예시 자료',
          if (fx.state == 'stale') '24시간 초과',
          '제공자 미검증',
        ].join(' · ')
      : '예시 자료 · 고정 값 · 현재 시세 아님',
  '관측 ${readableValuationTime(fx.observedAt)}',
  '가져옴 ${readableValuationTime(fx.fetchedAt)}',
  '저장 ${readableValuationTime(fx.recordedAt)}',
];

List<Widget> _fxTexts(BuildContext context, ValuationFx fx) {
  final facts = _fxFacts(fx);
  return [
    Text(facts.first, style: tabular(context)),
    for (final fact in facts.skip(1)) ...[
      const SizedBox(height: 4),
      Text(fact, style: Theme.of(context).textTheme.bodySmall),
    ],
  ];
}
