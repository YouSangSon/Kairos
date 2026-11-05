import 'package:flutter/material.dart';
import 'package:omni_folio_client/core/presentation/components.dart';
import 'package:omni_folio_client/features/connections/domain/models.dart';

class OverviewOrderRiskCard extends StatelessWidget {
  const OverviewOrderRiskCard({
    super.key,
    required this.orderLog,
    required this.retainedError,
    required this.busy,
    required this.onDetails,
  });

  final LocalOrderLog? orderLog;
  final bool retainedError;
  final bool busy;
  final VoidCallback onDetails;

  @override
  Widget build(BuildContext context) {
    Widget spaced(Widget child) =>
        Padding(padding: const EdgeInsets.only(top: 12), child: child);

    final value = orderLog;
    if (busy && value == null) {
      return spaced(
        const SectionCard(title: '로컬 주문 기록', child: Loading(compact: true)),
      );
    }
    if (value == null) {
      return spaced(
        SectionCard(
          title: '로컬 주문 기록',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Semantics(
                container: true,
                liveRegion: retainedError,
                child: Text(
                  retainedError
                      ? '주문 기록을 불러오지 못했습니다.'
                      : '아직 주문 기록을 확인하지 못했습니다.',
                ),
              ),
              const SizedBox(height: 4),
              Text(
                '로컬 주문 기록과 현재 브로커 상태는 별개입니다.',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 12),
              OutlinedButton(
                onPressed: onDetails,
                child: const Text('주문 기록 자세히 보기'),
              ),
            ],
          ),
        ),
      );
    }

    final submitUnknown = value.orders
        .where((order) => order.pendingAction == 'SUBMIT')
        .length;
    final cancelUnknown = value.orders
        .where((order) => order.pendingAction == 'CANCEL')
        .length;
    final unresolved = submitUnknown + cancelUnknown;
    if (unresolved == 0 && !busy && !retainedError) {
      return const SizedBox.shrink();
    }
    if (unresolved == 0) {
      final text = retainedError
          ? '새로고침에 실패해 마지막 정상 로컬 주문 기록을 유지합니다.'
          : '로컬 주문 기록을 다시 확인하는 중입니다.';
      return spaced(
        SectionCard(
          title: '로컬 주문 기록',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Semantics(container: true, liveRegion: true, child: Text(text)),
              const SizedBox(height: 4),
              Text(
                '현재 브로커 상태를 새로 확인한 결과가 아닙니다.',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 12),
              OutlinedButton(
                onPressed: onDetails,
                child: const Text('주문 기록 자세히 보기'),
              ),
            ],
          ),
        ),
      );
    }
    final unresolvedOrders = value.orders.where(
      (order) => order.pendingAction != 'none',
    );
    final lastRecordedAt = unresolvedOrders
        .map((order) => DateTime.parse(order.lastRecordedAt))
        .reduce((left, right) => left.isAfter(right) ? left : right);
    final status = '확인 필요한 주문 $unresolved건';
    final detail = '접수 미확정 $submitUnknown건 · 취소 미확정 $cancelUnknown건';
    final lastRecord = '마지막 로컬 기록 ${time(lastRecordedAt)}';
    final semantics =
        '로컬 주문 기록, 현재 브로커 상태 아님, $status, $detail, 재주문·추가 조작 금지, $lastRecord';
    return spaced(
      SectionCard(
        title: '로컬 주문 기록',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Semantics(
              container: true,
              excludeSemantics: true,
              liveRegion: unresolved > 0 || retainedError,
              label: semantics,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '로컬 주문 기록 · 현재 브로커 상태 아님',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                  const SizedBox(height: 8),
                  Text(status, style: Theme.of(context).textTheme.titleMedium),
                  const SizedBox(height: 4),
                  Text(detail, style: tabular(context)),
                  const SizedBox(height: 8),
                  Text(
                    '재주문·추가 조작 금지',
                    style: TextStyle(color: warningColor(context)),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    lastRecord,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ),
            ),
            if (busy) ...[
              const SizedBox(height: 12),
              Notice(
                icon: Icons.sync,
                text: '로컬 주문 기록을 다시 확인하는 중입니다.',
                color: warningColor(context),
              ),
            ],
            if (retainedError) ...[
              const SizedBox(height: 12),
              Notice(
                icon: Icons.warning_amber_rounded,
                text: '새로고침에 실패해 마지막 정상 주문 기록을 유지합니다.',
                color: Theme.of(context).colorScheme.error,
              ),
            ],
            const SizedBox(height: 12),
            OutlinedButton(
              onPressed: onDetails,
              child: const Text('주문 기록 자세히 보기'),
            ),
          ],
        ),
      ),
    );
  }
}

class OverviewReconciliationCard extends StatelessWidget {
  const OverviewReconciliationCard({
    super.key,
    required this.reconciliation,
    required this.retainedError,
    required this.busy,
    required this.onDetails,
  });

  final BrokerReconciliation? reconciliation;
  final bool retainedError;
  final bool busy;
  final VoidCallback onDetails;

  @override
  Widget build(BuildContext context) {
    final value = reconciliation;
    if (busy && value == null) {
      return const SectionCard(
        title: '증권사 잔고 대조',
        child: Loading(compact: true),
      );
    }
    if (value == null) {
      return SectionCard(
        title: '증권사 잔고 대조',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(retainedError ? '대조 결과를 불러오지 못했습니다.' : '아직 저장된 대조 기록이 없습니다.'),
            const SizedBox(height: 4),
            Text(
              '로컬 원장 확인과 증권사 잔고 대조는 별개입니다.',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 12),
            OutlinedButton(
              onPressed: onDetails,
              child: const Text('연결에서 자세히 보기'),
            ),
          ],
        ),
      );
    }

    final differences = value.positionDifferences;
    final mismatchCount = differences.where((item) => !item.match).length;
    final status = differences.isEmpty
        ? '대조할 보유 종목 없음'
        : value.allPositionsMatch
        ? '${differences.length}개 모두 일치'
        : '${differences.length}개 중 $mismatchCount개 불일치';
    final fetchedAt = time(DateTime.parse(value.fetchedAt));
    final recordedAt = time(DateTime.parse(value.recordedAt));
    final semantics =
        '증권사 잔고 대조, 마지막 저장 기록, 현재 상태 아님, $status, 증권사 $fetchedAt, 저장 $recordedAt';
    return SectionCard(
      title: '증권사 잔고 대조',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Semantics(
            container: true,
            excludeSemantics: true,
            label: semantics,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '마지막 저장 기록 · 현재 상태 아님',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
                const SizedBox(height: 8),
                Text(status, style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: 4),
                Text(
                  '증권사 $fetchedAt · 저장 $recordedAt',
                  style: tabular(context),
                ),
              ],
            ),
          ),
          if (busy) ...[
            const SizedBox(height: 12),
            const _ReconciliationRefreshingNotice(),
          ],
          if (retainedError) ...[
            const SizedBox(height: 12),
            Notice(
              icon: Icons.warning_amber_rounded,
              text: '새로고침에 실패해 마지막 정상 대조 기록을 유지합니다.',
              color: Theme.of(context).colorScheme.error,
            ),
          ],
          const SizedBox(height: 12),
          OutlinedButton(
            onPressed: onDetails,
            child: const Text('연결에서 자세히 보기'),
          ),
        ],
      ),
    );
  }
}

class LocalOrderLogCard extends StatelessWidget {
  const LocalOrderLogCard({
    super.key,
    required this.orderLog,
    required this.retainedError,
    required this.busy,
    required this.onRefresh,
  });

  final LocalOrderLog orderLog;
  final bool retainedError;
  final bool busy;
  final VoidCallback onRefresh;

  @override
  Widget build(BuildContext context) => SectionCard(
    title: '로컬 주문 기록',
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Notice(
          icon: Icons.history,
          text: '로컬 주문 기록 · 현재 브로커 상태가 아닙니다.\n이 화면은 증권사를 새로 조회하지 않습니다.',
          color: warningColor(context),
        ),
        if (orderLog.orders.isEmpty) ...[
          const SizedBox(height: 12),
          const Text('아직 저장된 주문 기록이 없습니다'),
        ],
        for (final order in orderLog.orders) ...[
          const Divider(height: 24),
          _LocalOrderRow(order: order),
        ],
        if (busy) ...[
          const SizedBox(height: 12),
          Notice(
            icon: Icons.sync,
            text: '로컬 주문 기록을 다시 확인하는 중입니다. 마지막 정상 기록을 유지합니다.',
            color: warningColor(context),
          ),
        ],
        if (retainedError) ...[
          const SizedBox(height: 12),
          Notice(
            icon: Icons.warning_amber_rounded,
            text: '새로고침 실패: 주문 기록을 불러오지 못했습니다.\n마지막 정상 주문 기록은 유지됩니다.',
            color: Theme.of(context).colorScheme.error,
          ),
        ],
        const SizedBox(height: 12),
        ElevatedButton.icon(
          onPressed: busy ? null : onRefresh,
          icon: const Icon(Icons.refresh),
          label: const Text('로컬 주문 기록 다시 불러오기'),
        ),
      ],
    ),
  );
}

class _LocalOrderRow extends StatelessWidget {
  const _LocalOrderRow({required this.order});

  final LocalOrderView order;

  @override
  Widget build(BuildContext context) {
    final mode = order.mode == 'synthetic' ? '합성 테스트' : '로컬 페이퍼';
    final side = order.side == 'BUY' ? '매수' : '매도';
    final status = _localOrderStatus(order.status, order.pendingAction);
    final semanticsStatus =
        order.pendingAction == 'CANCEL' && order.status == 'FILLED'
        ? '체결됨, 취소 결과 확인 중, 추가 조작 금지'
        : order.status == 'SUBMIT_UNKNOWN'
        ? '브로커 결과 미확정, 재주문 금지'
        : status.$1;
    return Semantics(
      container: true,
      excludeSemantics: true,
      label:
          '$mode 주문, ${order.symbol} $side 지정가, 주문 수량 ${order.quantity}, 체결 수량 ${order.filledQuantity}, ${order.currency} ${order.limitPrice}, $semanticsStatus',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(status.$1, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(status.$2),
          const SizedBox(height: 8),
          Text('$mode · ${order.symbol} · $side · 지정가'),
          const SizedBox(height: 4),
          Text(
            '주문 ${order.quantity}주 · 체결 ${order.filledQuantity}주\n${order.currency} ${order.limitPrice} · ${time(DateTime.parse(order.lastRecordedAt))}',
            style: tabular(context),
          ),
        ],
      ),
    );
  }
}

(String, String) _localOrderStatus(String status, String pendingAction) {
  if (pendingAction == 'CANCEL' && status == 'FILLED') {
    return (
      '체결됨 · 취소 결과 확인 중',
      '주문은 로컬 기록상 전량 체결됐지만 증권사 취소 결과를 아직 확인하지 못했습니다. 추가 조작 금지.',
    );
  }
  return switch (status) {
    'RECORDED' => ('로컬 기록 저장', '주문 의도가 로컬 로그에 저장되었습니다.'),
    'READY' => ('로컬 전송 준비', '위험 검사를 통과한 로컬 기록입니다.'),
    'RISK_REJECTED' => ('위험 검사 차단', '위험 규칙이 주문 전송을 차단했습니다.'),
    'SUBMIT_UNKNOWN' => (
      '브로커 결과 미확정 · 재주문 금지',
      '증권사 접수 결과를 아직 확인하지 못했습니다. 같은 주문을 다시 보내면 안 됩니다.',
    ),
    'OPEN' => ('로컬 기록: 접수', '저장된 이벤트에서 접수 상태로 재생되었습니다.'),
    'REJECTED' => ('로컬 기록: 거절', '저장된 이벤트에서 거절 상태로 재생되었습니다.'),
    'PARTIALLY_FILLED' => ('로컬 기록: 일부 체결', '저장된 이벤트에서 일부 체결로 재생되었습니다.'),
    'CANCEL_UNKNOWN' => ('취소 결과 미확정 · 추가 조작 금지', '증권사 취소 결과를 아직 확인하지 못했습니다.'),
    'CANCELED' => ('로컬 기록: 취소', '저장된 이벤트에서 취소 상태로 재생되었습니다.'),
    'FILLED' => ('로컬 기록: 체결', '저장된 이벤트에서 체결 상태로 재생되었습니다.'),
    _ => throw StateError('validated local order status required'),
  };
}

class BrokerReconciliationCard extends StatelessWidget {
  const BrokerReconciliationCard({
    super.key,
    required this.reconciliation,
    required this.retainedError,
    required this.busy,
    required this.onRefresh,
  });

  final BrokerReconciliation reconciliation;
  final bool retainedError;
  final bool busy;
  final VoidCallback onRefresh;

  @override
  Widget build(BuildContext context) {
    final differences = reconciliation.positionDifferences;
    final mismatchCount = differences.where((item) => !item.match).length;
    final status = reconciliation.allPositionsMatch
        ? '일치 · ${differences.length}종목의 보유 수량이 원장과 같습니다.'
        : '불일치 · $mismatchCount/${differences.length}종목의 보유 수량이 다릅니다.';
    final environment = reconciliation.environment == 'mock' ? '모의투자' : '실전';
    return SectionCard(
      title: '증권사 잔고 대조',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Notice(
            icon: Icons.history,
            text:
                '마지막 저장 스냅샷 · 현재 상태 아님\n현재 대조 범위는 보유 수량입니다. 현금, 평가금액, 수수료, 주문 체결은 대조하지 않았습니다.',
            color: warningColor(context),
          ),
          const SizedBox(height: 12),
          Text('키움 · $environment · ${reconciliation.exchange}'),
          const SizedBox(height: 4),
          Text(
            '증권사 ${time(DateTime.parse(reconciliation.fetchedAt))} · 저장 ${time(DateTime.parse(reconciliation.recordedAt))}\n원장 ${reconciliation.ledgerRevision}',
            style: tabular(context),
          ),
          const SizedBox(height: 12),
          Semantics(
            container: true,
            label: status,
            child: Text(status, style: Theme.of(context).textTheme.titleMedium),
          ),
          if (differences.isEmpty) ...[
            const SizedBox(height: 8),
            const Text('현재 대조할 보유 종목이 없습니다.'),
          ],
          for (final difference in differences) ...[
            const Divider(height: 24),
            _BrokerDifferenceRow(difference: difference),
          ],
          if (busy) ...[
            const SizedBox(height: 12),
            const _ReconciliationRefreshingNotice(),
          ],
          if (retainedError) ...[
            const SizedBox(height: 12),
            Notice(
              icon: Icons.warning_amber_rounded,
              text: '대조 결과 새로고침에 실패했습니다.\n마지막 정상 대조 기록은 유지됩니다.',
              color: Theme.of(context).colorScheme.error,
            ),
          ],
          const SizedBox(height: 12),
          ElevatedButton.icon(
            onPressed: busy ? null : onRefresh,
            icon: const Icon(Icons.refresh),
            label: const Text('저장된 대조 다시 불러오기'),
          ),
        ],
      ),
    );
  }
}

class _ReconciliationRefreshingNotice extends StatelessWidget {
  const _ReconciliationRefreshingNotice();

  static const text = '저장된 대조를 다시 불러오는 중입니다. 마지막 정상 기록을 유지합니다.';

  @override
  Widget build(BuildContext context) => Semantics(
    container: true,
    excludeSemantics: true,
    liveRegion: true,
    label: text,
    child: Notice(icon: Icons.sync, text: text, color: warningColor(context)),
  );
}

class _BrokerDifferenceRow extends StatelessWidget {
  const _BrokerDifferenceRow({required this.difference});
  final BrokerPositionDifference difference;

  @override
  Widget build(BuildContext context) {
    final status = difference.match ? '일치' : '불일치';
    final semantics =
        '${difference.symbol} $status, 증권사 수량 ${difference.brokerQuantity}, 원장 수량 ${difference.ledgerQuantity}, 차이 ${difference.difference}';
    return Semantics(
      container: true,
      excludeSemantics: true,
      label: semantics,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('${difference.symbol} · $status'),
          const SizedBox(height: 4),
          Text(
            '증권사 ${difference.brokerQuantity} · 원장 ${difference.ledgerQuantity} · 차이 ${difference.difference}',
            style: tabular(context),
          ),
        ],
      ),
    );
  }
}
