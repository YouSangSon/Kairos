import 'package:flutter/material.dart';
import 'package:omni_folio_client/core/domain/models.dart';
import 'package:omni_folio_client/core/presentation/components.dart';
import 'package:omni_folio_client/features/connections/domain/models.dart';
import 'package:omni_folio_client/features/portfolio/application/portfolio_controller.dart';
import 'package:omni_folio_client/features/portfolio/domain/models.dart';
import 'package:omni_folio_client/features/portfolio/presentation/holding_valuation_page.dart';
import 'package:omni_folio_client/features/portfolio/presentation/portfolio_xirr_page.dart';

class OverviewPage extends StatelessWidget {
  const OverviewPage({
    super.key,
    required this.controller,
    required this.onImport,
    required this.onHoldings,
    required this.onAsset,
    required this.orderRisk,
    required this.reconciliationCard,
  });
  final PortfolioController controller;
  final VoidCallback onImport;
  final VoidCallback onHoldings;
  final void Function(BuildContext, Holding) onAsset;
  final Widget orderRisk, reconciliationCard;

  @override
  Widget build(BuildContext context) {
    if (controller.state == DataState.loading) return const Loading();
    if (controller.state == DataState.error) {
      return _FallbackWithOrderRisk(
        orderRisk: orderRisk,
        message: Message(
          icon: Icons.cloud_off,
          title: '스냅샷을 불러오지 못했습니다',
          body:
              '${controller.error ?? '알 수 없는 오류'}\n저장된 데이터는 없으며 원장은 변경되지 않았습니다.',
          action: controller.refresh,
          actionLabel: '다시 시도',
        ),
      );
    }
    final snapshot = controller.snapshot;
    final isFirstRunEmpty =
        controller.state == DataState.neverVerified &&
        snapshot != null &&
        snapshot.cash.isEmpty &&
        snapshot.holdings.isEmpty &&
        snapshot.realizedPnl.isEmpty;
    if (snapshot == null || isFirstRunEmpty) {
      return _FallbackWithOrderRisk(
        orderRisk: orderRisk,
        message: Message(
          icon: Icons.inbox_outlined,
          title: isFirstRunEmpty ? '첫 거래 내역을 가져오세요' : '아직 스냅샷이 없습니다',
          body: '거래 내역을 미리 확인한 뒤 적용하면 현금과 보유 내역이 표시됩니다.',
          action: onImport,
          actionLabel: '거래 내역 가져오기',
        ),
      );
    }
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
      children: [
        Text('내 투자 기록', style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 12),
        _TrustBanner(
          state: controller.state,
          status: controller.status,
          retainedError: controller.error,
          reconciliation: controller.reconciliation,
          reconciliationFailed: controller.reconciliationFailed,
          reconciliationBusy: controller.reconciliationBusy,
        ),
        orderRisk,
        const SizedBox(height: 12),
        _CurrentPortfolioValuationCard(
          valuation: controller.currentValuation,
          snapshotRevision: snapshot.ledgerRevision,
          busy: controller.currentValuationBusy,
          retainedError: controller.currentValuationFailed,
        ),
        const SizedBox(height: 12),
        _PortfolioHero(snapshot: snapshot),
        const SizedBox(height: 12),
        OutlinedButton.icon(
          onPressed: () => Navigator.of(context).push<void>(
            MaterialPageRoute(
              builder: (_) => PortfolioXirrPage(api: controller.api),
            ),
          ),
          icon: const Icon(Icons.trending_up),
          label: const Text('입출금을 반영한 수익률 보기', textAlign: TextAlign.center),
        ),
        const SizedBox(height: 12),

        if (snapshot.holdings.isNotEmpty) ...[
          const SizedBox(height: 16),
          SectionCard(
            title: '보유 자산',
            child: Column(
              children: [
                for (final holding in snapshot.holdings.take(3)) ...[
                  _HoldingRow(
                    holding: holding,
                    onTap: () => onAsset(context, holding),
                  ),
                  const Divider(height: 1),
                ],
                if (snapshot.holdings.length > 3)
                  TextButton(
                    onPressed: onHoldings,
                    child: Text('보유 탭에서 전체 ${snapshot.holdings.length}종목 보기'),
                  ),
              ],
            ),
          ),
        ],
        const SizedBox(height: 12),
        reconciliationCard,
      ],
    );
  }
}

class HoldingsPage extends StatelessWidget {
  const HoldingsPage({
    super.key,
    required this.controller,
    required this.onAsset,
  });
  final void Function(BuildContext, Holding) onAsset;
  final PortfolioController controller;

  @override
  Widget build(BuildContext context) {
    final holdings = controller.snapshot?.holdings;
    if (holdings == null || holdings.isEmpty) {
      return ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
        children: [
          _header(context, holdings?.length),
          const SizedBox(height: 16),
          SectionCard(
            title: holdings == null ? '보유 데이터 없음' : '보유 종목 없음',
            child: Text(
              holdings == null
                  ? '홈에서 데이터를 새로고침하세요.'
                  : '적용된 거래가 없거나 모두 매도되었습니다.',
            ),
          ),
        ],
      );
    }
    return ListView.separated(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
      itemCount: holdings.length + 1,
      separatorBuilder: (_, index) => SizedBox(height: index == 0 ? 16 : 8),
      itemBuilder: (context, index) {
        if (index == 0) {
          return _header(context, holdings.length);
        }
        final holding = holdings[index - 1];
        return Card(
          clipBehavior: Clip.antiAlias,
          child: _HoldingRow(
            holding: holding,
            onTap: () => onAsset(context, holding),
          ),
        );
      },
    );
  }

  Widget _header(BuildContext context, int? count) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(
        count == null ? '보유 자산' : '$count종목',
        style: Theme.of(context).textTheme.headlineMedium,
      ),
      const SizedBox(height: 4),
      Text(
        '로컬 원장의 수량과 FIFO 원가 기준입니다.',
        style: Theme.of(context).textTheme.bodySmall,
      ),
      const SizedBox(height: 12),
      OutlinedButton(
        onPressed: () => Navigator.of(context).push(
          MaterialPageRoute<void>(
            builder: (_) => HoldingValuationPage(api: controller.api),
          ),
        ),
        child: const Text('저장 가격으로 평가 보기', textAlign: TextAlign.center),
      ),
    ],
  );
}

class _PortfolioHero extends StatelessWidget {
  const _PortfolioHero({required this.snapshot});
  final PortfolioSnapshot snapshot;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Semantics(
              container: true,
              header: true,
              child: Text(
                '현금 잔액',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              moneySummary(snapshot.cash),
              style: Theme.of(context).textTheme.headlineMedium,
              semanticsLabel: '현금 ${moneySummary(snapshot.cash)}',
            ),
            const SizedBox(height: 16),
            const Divider(height: 1),
            const SizedBox(height: 12),
            Text(
              '최근 원장 기록 · 이벤트 ${snapshot.eventCount}건 · 가져오기 ${snapshot.receiptCount}건',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 4),
            Text(time(snapshot.recordedAt), style: tabular(context)),
            const SizedBox(height: 16),
            Wrap(
              spacing: 24,
              runSpacing: 12,
              children: [
                _HeroMetric(
                  label: '보유 자산',
                  value: '${snapshot.holdings.length}종목',
                ),
                _RealizedPnl(snapshot.realizedPnl),
              ],
            ),
            const SizedBox(height: 12),
            Text('평가금액 미포함', style: Theme.of(context).textTheme.bodySmall),
          ],
        ),
      ),
    );
  }
}

class _CurrentPortfolioValuationCard extends StatelessWidget {
  const _CurrentPortfolioValuationCard({
    required this.valuation,
    required this.snapshotRevision,
    required this.busy,
    required this.retainedError,
  });

  final CurrentPortfolioValuation? valuation;
  final String snapshotRevision;
  final bool busy;
  final bool retainedError;

  @override
  Widget build(BuildContext context) {
    final current = valuation;
    if (current == null) {
      final title = busy ? '전체 평가 계산 중' : '전체 평가를 불러오지 못했습니다';
      final body = busy
          ? '같은 원장 시점의 저장 가격과 직접 환율을 확인하고 있습니다.'
          : '현금과 보유 내역은 계속 볼 수 있습니다. 새로고침하면 다시 계산합니다.';
      return Card(
        child: Semantics(
          container: true,
          liveRegion: !busy,
          excludeSemantics: true,
          label: '$title. $body',
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  busy ? Icons.hourglass_top_rounded : Icons.info_outline,
                  color: busy ? null : warningColor(context),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        title,
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 4),
                      Text(body, style: Theme.of(context).textTheme.bodySmall),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    }

    final mismatch = current.ledgerRevision != snapshotRevision;
    final valuationTimeText = time(DateTime.parse(current.valuationAsOf));
    final total = current.totalValue;
    if (total == null) {
      final empty = current.status == 'empty';
      final title = empty ? '평가할 자산 없음' : '전체 평가를 계산할 수 없음';
      final body = empty
          ? '이 평가 시점의 원장에는 평가할 현금과 보유 자산이 없습니다.'
          : '필요한 저장 가격 또는 직접 환율 ${current.missingInputCount}건을 확인하세요. 부분 합계는 표시하지 않습니다.';
      final notices = [
        if (current.ownerDeclaredFx) '직접 입력 환율 사용 · 시세 제공자가 검증한 값이 아닙니다.',
        if (current.ownerDeclaredPrice) '직접 입력 가격 사용 · 시세 제공자가 검증한 값이 아닙니다.',
        if (current.sample) '예시 자료 포함 · 실제 계좌 평가가 아닙니다.',
        if (mismatch) '원장 갱신 전 저장 평가',
        if (busy) '새 평가 계산 중 · 현재 표시 상태 유지',
        if (retainedError) '새로고침 실패 · 마지막 정상 상태 유지',
      ];
      return Card(
        child: Semantics(
          container: true,
          liveRegion: true,
          excludeSemantics: true,
          label: [
            title,
            body,
            ...notices,
            '평가 시각 $valuationTimeText',
          ].join('\n'),
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: 6),
                Text(body),
                for (final notice in notices) ...[
                  const SizedBox(height: 8),
                  Text(
                    notice,
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: warningColor(context),
                    ),
                  ),
                ],
                const SizedBox(height: 8),
                Text(
                  '평가 시각 $valuationTimeText',
                  style: tabular(context).copyWith(
                    fontSize: Theme.of(context).textTheme.bodySmall?.fontSize,
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    }

    final totalText = formattedMoney(total);
    final cashText = formattedMoney(current.cashValue!);
    final holdingsText = formattedMoney(current.holdingsValue!);
    final isSample = current.status == 'stale_sample';
    final authority = isSample
        ? '저장된 로컬 샘플 기준 · 실시간·증권사 값 아님'
        : '같은 원장 시점의 KRW 평가 · 증권사 잔고 값 아님';
    final notices = [
      authority,
      if (current.ownerDeclaredFx) '직접 입력 환율 사용 · 시세 제공자가 검증한 값이 아닙니다.',
      if (current.ownerDeclaredPrice) '직접 입력 가격 사용 · 시세 제공자가 검증한 값이 아닙니다.',
      if (mismatch) '원장 갱신 전 저장 평가',
      if (busy) '새 평가 계산 중 · 현재 표시값 유지',
      if (retainedError) '새로고침 실패 · 마지막 정상 평가 유지',
    ];
    return Card(
      child: Semantics(
        container: true,
        excludeSemantics: true,
        label:
            '총 자산 $totalText. 현금 $cashText. 보유 자산 $holdingsText. ${notices.join('. ')}. 평가 시각 $valuationTimeText',
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('총 자산', style: Theme.of(context).textTheme.bodySmall),
              const SizedBox(height: 4),
              Text(
                totalText,
                style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                  fontFeatures: const [FontFeature.tabularFigures()],
                  fontWeight: FontWeight.w800,
                ),
              ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 24,
                runSpacing: 8,
                children: [
                  _HeroMetric(label: '현금', value: cashText),
                  _HeroMetric(label: '보유 자산', value: holdingsText),
                ],
              ),
              const SizedBox(height: 12),
              for (final notice in notices) ...[
                Text(
                  notice,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: notice == authority && !isSample
                        ? null
                        : warningColor(context),
                  ),
                ),
                const SizedBox(height: 4),
              ],
              Text(
                '평가 시각 $valuationTimeText',
                style: tabular(context).copyWith(
                  fontSize: Theme.of(context).textTheme.bodySmall?.fontSize,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _HeroMetric extends StatelessWidget {
  const _HeroMetric({required this.label, required this.value});
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label),
        const SizedBox(height: 2),
        Text(
          value,
          style: tabular(context).copyWith(fontWeight: FontWeight.w700),
        ),
      ],
    );
  }
}

class _RealizedPnl extends StatelessWidget {
  const _RealizedPnl(this.money);
  final List<Money> money;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Text('실현 손익'),
        const SizedBox(height: 2),
        if (money.isEmpty)
          Text('실현 변동 없음', style: tabular(context))
        else
          for (final item in money)
            Text(
              '${isZero(item.amount)
                  ? '실현 변동 없음'
                  : item.amount.startsWith('-')
                  ? '실현 손실'
                  : '실현 이익'} ${item.currency} ${isZero(item.amount)
                  ? '0'
                  : item.amount.startsWith('-')
                  ? item.amount
                  : '+${item.amount}'}',
              style: tabular(context).copyWith(
                color: isZero(item.amount)
                    ? Theme.of(context).colorScheme.onSurface
                    : item.amount.startsWith('-')
                    ? Theme.of(context).colorScheme.error
                    : positiveColor(context),
                fontWeight: FontWeight.w700,
              ),
            ),
      ],
    );
  }
}

class _HoldingRow extends StatelessWidget {
  const _HoldingRow({required this.holding, required this.onTap});
  final Holding holding;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final largeText = MediaQuery.textScalerOf(context).scale(16) > 22;
      final details = Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(holding.symbol, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(
            largeText
                ? '수량 ${holding.quantity} · 원가 ${holding.currency} ${holding.costBasis}'
                : '수량 ${holding.quantity}',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      );
      return Semantics(
        button: true,
        excludeSemantics: true,
        onTap: onTap,
        label:
            '${holding.symbol}, 수량 ${holding.quantity}, 원가 ${holding.currency} ${holding.costBasis}',
        child: InkWell(
          onTap: onTap,
          child: ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 72),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
              child: Row(
                children: [
                  CircleAvatar(
                    radius: 20,
                    backgroundColor: Theme.of(
                      context,
                    ).colorScheme.primary.withValues(alpha: 0.12),
                    child: Text(
                      holding.symbol.substring(0, 1),
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.primary,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: largeText
                        ? details
                        : Row(
                            children: [
                              Expanded(child: details),
                              const SizedBox(width: 12),
                              Column(
                                crossAxisAlignment: CrossAxisAlignment.end,
                                children: [
                                  Text(
                                    '${holding.currency} ${holding.costBasis}',
                                    style: tabular(
                                      context,
                                    ).copyWith(fontWeight: FontWeight.w700),
                                  ),
                                  const SizedBox(height: 4),
                                  Text(
                                    '원가',
                                    style: Theme.of(
                                      context,
                                    ).textTheme.bodySmall,
                                  ),
                                ],
                              ),
                            ],
                          ),
                  ),
                  const SizedBox(width: 4),
                  const Icon(Icons.chevron_right_rounded),
                ],
              ),
            ),
          ),
        ),
      );
    },
  );
}

class _FallbackWithOrderRisk extends StatelessWidget {
  const _FallbackWithOrderRisk({
    required this.orderRisk,
    required this.message,
  });

  final Widget orderRisk;
  final Widget message;

  @override
  Widget build(BuildContext context) => ListView(
    padding: const EdgeInsets.all(16),
    children: [message, const SizedBox(height: 12), orderRisk],
  );
}

class _TrustBanner extends StatelessWidget {
  const _TrustBanner({
    required this.state,
    required this.status,
    required this.retainedError,
    required this.reconciliation,
    required this.reconciliationFailed,
    required this.reconciliationBusy,
  });
  final DataState state;
  final ServiceStatus? status;
  final String? retainedError;
  final BrokerReconciliation? reconciliation;
  final bool reconciliationFailed;
  final bool reconciliationBusy;

  @override
  Widget build(BuildContext context) {
    final isGood = state == DataState.success;
    final color = switch (state) {
      DataState.success => positiveColor(context),
      DataState.neverVerified => warningColor(context),
      DataState.stale => warningColor(context),
      _ => Theme.of(context).colorScheme.error,
    };
    final label = switch (state) {
      DataState.success => '로컬 기록 확인 완료',
      DataState.neverVerified => '아직 확인되지 않음',
      DataState.stale => '새로고침 필요',
      DataState.partial => '일부 데이터 확인 필요',
      _ => '확인 필요',
    };
    final verified = status?.lastVerifiedAt == null
        ? '없음'
        : time(status!.lastVerifiedAt!);
    final error = retainedError == null
        ? ''
        : '\n새로고침 실패: ${retainedError!}\n마지막 정상 스냅샷은 유지됩니다.';
    final reconciliationText = reconciliation == null
        ? reconciliationBusy
              ? '증권사 잔고 대조 확인 중'
              : !reconciliationFailed
              ? '증권사 잔고 대조 기록 없음'
              : '증권사 잔고 대조 확인 실패'
        : reconciliationBusy
        ? '증권사 잔고 대조 저장 기록 다시 확인 중 · 현재 상태 아님'
        : '증권사 잔고 대조 저장 기록 · 현재 상태 아님';
    final text =
        '데이터 상태: $label\n로컬 원장 마지막 확인 $verified\n$reconciliationText$error';
    return Semantics(
      container: true,
      excludeSemantics: true,
      liveRegion: !isGood,
      label: text,
      child: Notice(
        icon: isGood ? Icons.verified : Icons.warning_amber_rounded,
        color: color,
        text: text,
      ),
    );
  }
}
