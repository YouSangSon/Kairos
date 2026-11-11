import 'package:flutter/material.dart';
import 'package:omni_folio_client/core/presentation/components.dart';
import 'package:omni_folio_client/features/market_data/domain/market_data_repository.dart';
import 'package:omni_folio_client/features/market_data/domain/models.dart';
import 'package:omni_folio_client/features/portfolio/domain/models.dart';

class AssetDetailPage extends StatefulWidget {
  const AssetDetailPage({
    super.key,
    required this.api,
    required this.holding,
    required this.onLedger,
    required this.onResearch,
  });
  final void Function(BuildContext) onLedger, onResearch;
  final MarketDataRepository api;
  final Holding holding;

  @override
  State<AssetDetailPage> createState() => _AssetDetailPageState();
}

class _AssetDetailPageState extends State<AssetDetailPage> {
  MarketCandles? _candles;
  String? _error;
  var _busy = true;
  var _tableVisible = false;
  var _range = _ChartRange.all;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    if (_busy && _candles != null) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final candles = await widget.api.candles(widget.holding.symbol);
      if (candles.symbol != widget.holding.symbol ||
          (candles.instrumentId != null &&
              (candles.instrumentId != widget.holding.instrumentId ||
                  candles.currency != widget.holding.currency))) {
        throw const FormatException('Market series does not match holding');
      }
      if (mounted) setState(() => _candles = candles);
    } catch (_) {
      if (mounted) {
        setState(() => _error = '시세를 불러오지 못했습니다. 다시 시도하세요.');
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: Text(
        '${widget.holding.symbol} 시세',
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      actions: [
        IconButton(
          tooltip: '원장 평균단가·매매 보기',
          onPressed: () => widget.onLedger(context),
          icon: const Icon(Icons.receipt_long_outlined),
        ),
        IconButton(
          tooltip: '공시·뉴스·분석 보기',
          onPressed: () => widget.onResearch(context),
          icon: const Icon(Icons.article_outlined),
        ),
        IconButton(
          tooltip: '차트 새로고침',
          onPressed: _busy ? null : _refresh,
          icon: const Icon(Icons.refresh),
        ),
      ],
    ),
    body: SafeArea(child: _body(context)),
  );

  Widget _body(BuildContext context) {
    if (_busy && _candles == null) return const Loading();
    if (_candles == null) {
      return Message(
        icon: Icons.cloud_off,
        title: '시세를 불러오지 못했습니다',
        body: '${_error ?? '알 수 없는 오류'}\n원천/원천 기준/가져온 시각: 확인할 수 없음',
        action: _refresh,
        actionLabel: '다시 시도',
      );
    }
    final candles = _candles!;
    final visibleBars = _rangeBars(candles.bars);
    return ListView(
      key: const Key('asset-detail-scroll'),
      padding: const EdgeInsets.all(16),
      children: [
        if (candles.sample) ...[
          Semantics(
            container: true,
            liveRegion: true,
            child: Notice(
              icon: Icons.science_outlined,
              text: '샘플 데이터 · 실시간 아님',
              color: warningColor(context),
            ),
          ),
          const SizedBox(height: 12),
        ],
        SectionCard(
          title: '${candles.symbol} · ${candles.interval}',
          child: _MarketMetadata(candles: candles),
        ),
        const SizedBox(height: 12),
        if (_error != null) ...[
          Semantics(
            key: const Key('market-refresh-error'),
            container: true,
            excludeSemantics: true,
            liveRegion: true,
            label: '새로고침 실패: $_error. 마지막 정상 시세는 유지됩니다.',
            child: Notice(
              icon: Icons.error_outline,
              text: '새로고침 실패: $_error\n마지막 정상 시세는 유지됩니다.',
              color: Theme.of(context).colorScheme.error,
            ),
          ),
          const SizedBox(height: 12),
        ],
        if (candles.state == 'empty')
          const Message(
            icon: Icons.show_chart,
            title: '표시할 봉이 없습니다',
            body: '선택한 종목의 일봉 데이터가 아직 없습니다.',
          )
        else ...[
          if (candles.state == 'stale' || candles.state == 'partial') ...[
            Notice(
              icon: candles.state == 'stale'
                  ? Icons.schedule
                  : Icons.warning_amber_rounded,
              text: candles.state == 'stale'
                  ? '오래된 시세입니다. 투자 판단 전 원천과 시각을 확인하세요.'
                  : '일부 시세입니다. 누락 또는 경고를 확인하세요.',
              color: warningColor(context),
            ),
            const SizedBox(height: 12),
          ],
          if (candles.issues.isNotEmpty) ...[
            Notice(
              icon: Icons.info_outline,
              text: candles.issues
                  .map((issue) => '${issue.code}: ${issue.message}')
                  .join('\n'),
              color: warningColor(context),
            ),
            const SizedBox(height: 12),
          ],
          _CandleChart(
            candles: candles,
            bars: visibleBars,
            range: _range,
            onRangeSelected: (range) => setState(() => _range = range),
          ),
          const SizedBox(height: 12),
          OutlinedButton.icon(
            onPressed: () => setState(() => _tableVisible = !_tableVisible),
            icon: Icon(_tableVisible ? Icons.expand_less : Icons.table_rows),
            label: Text(_tableVisible ? 'OHLCV 표 닫기' : '정확한 OHLCV 표 보기'),
          ),
          if (_tableVisible) ...[
            const SizedBox(height: 12),
            _OhlcvTable(bars: visibleBars),
          ],
        ],
      ],
    );
  }

  List<MarketBar> _rangeBars(List<MarketBar> bars) {
    final days = switch (_range) {
      _ChartRange.days30 => 30,
      _ChartRange.days90 => 90,
      _ChartRange.days365 => 365,
      _ChartRange.all => null,
    };
    if (bars.isEmpty || days == null) return bars;
    final cutoff = DateTime.parse(bars.last.at).subtract(Duration(days: days));
    return bars
        .where((bar) => !DateTime.parse(bar.at).isBefore(cutoff))
        .toList(growable: false);
  }
}

enum _ChartRange { days30, days90, days365, all }

class _MarketMetadata extends StatelessWidget {
  const _MarketMetadata({required this.candles});
  final MarketCandles candles;

  @override
  Widget build(BuildContext context) {
    final priceAdjustment = switch (candles.priceAdjustment) {
      'provider_adjusted' => '공급자 조정 가격 · 조정 정확성 미검증',
      _ => '조정 여부 확인 안 됨',
    };
    return Text(
      '거래소 ${candles.venue}\n가격 통화 ${candles.currency ?? '확인 안 됨'}\n'
      '${candles.instrumentId == null ? '원장 종목과의 연결 확인 안 됨' : '자료에 명시된 종목·통화가 원장 종목과 일치'}\n'
      '시간대 ${candles.timezone} · 봉 시각 의미 확인 안 됨\n'
      '가격 기준 $priceAdjustment\n'
      '평균단가·매매 기록을 겹쳐 표시할 수 없습니다. 가격 조정과 봉 시각의 의미를 확인해야 합니다.\n'
      '원천 ${candles.source}\n원천 기준 ${candles.sourceAsOf ?? '없음'}\n가져온 시각 ${candles.fetchedAt}\n상태 ${candles.state}',
      style: tabular(context),
    );
  }
}

class _CandleChart extends StatelessWidget {
  const _CandleChart({
    required this.candles,
    required this.bars,
    required this.range,
    required this.onRangeSelected,
  });
  final MarketCandles candles;
  final List<MarketBar> bars;
  final _ChartRange range;
  final ValueChanged<_ChartRange> onRangeSelected;

  @override
  Widget build(BuildContext context) {
    final rangeLabel = switch (range) {
      _ChartRange.days30 => '최근 30일',
      _ChartRange.days90 => '최근 90일',
      _ChartRange.days365 => '최근 365일',
      _ChartRange.all => '전체 기간',
    };
    Widget choice(String label, String semanticsLabel, _ChartRange value) {
      final selected = range == value;
      final key = switch (value) {
        _ChartRange.days30 => const Key('chart-range-30d'),
        _ChartRange.days90 => const Key('chart-range-90d'),
        _ChartRange.days365 => const Key('chart-range-365d'),
        _ChartRange.all => const Key('chart-range-all'),
      };
      return Semantics(
        key: key,
        label: semanticsLabel,
        button: true,
        selected: selected,
        onTap: () => onRangeSelected(value),
        excludeSemantics: true,
        child: ConstrainedBox(
          constraints: const BoxConstraints(minHeight: 48),
          child: ChoiceChip(
            label: Text(label),
            selected: selected,
            onSelected: (_) => onRangeSelected(value),
          ),
        ),
      );
    }

    final summary =
        '${candles.symbol} ${candles.interval} 캔들 차트. '
        '$rangeLabel, ${bars.length}개 봉, 상태 ${candles.state}. '
        '${bars.first.at}부터 ${bars.last.at}까지. '
        '시가 ${bars.first.open}, 종가 ${bars.last.close}. '
        '정확한 OHLCV 표 보기 버튼으로 선택한 범위의 모든 값을 확인할 수 있습니다.';
    return SectionCard(
      title: '시세 차트',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('표시 범위', style: Theme.of(context).textTheme.titleSmall),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              choice('30일', '최근 30일', _ChartRange.days30),
              choice('90일', '최근 90일', _ChartRange.days90),
              choice('365일', '최근 365일', _ChartRange.days365),
              choice('전체', '전체 기간', _ChartRange.all),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            '$rangeLabel · ${bars.first.at} ~ ${bars.last.at} · ${bars.length}개 봉',
            style: tabular(context),
          ),
          const SizedBox(height: 12),
          Semantics(
            container: true,
            label: summary,
            child: ExcludeSemantics(
              child: LayoutBuilder(
                builder: (context, constraints) {
                  final minimumWidth = bars.length * 4.0;
                  final chartWidth = minimumWidth > constraints.maxWidth
                      ? minimumWidth
                      : constraints.maxWidth;
                  return SingleChildScrollView(
                    scrollDirection: Axis.horizontal,
                    child: SizedBox(
                      height: 260,
                      width: chartWidth,
                      child: RepaintBoundary(
                        child: CustomPaint(
                          painter: _CandlePainter(
                            bars: bars,
                            bull: positiveColor(context),
                            bear: Theme.of(context).colorScheme.error,
                            axis: Theme.of(
                              context,
                            ).colorScheme.onSurface.withValues(alpha: 0.35),
                          ),
                        ),
                      ),
                    ),
                  );
                },
              ),
            ),
          ),
          const SizedBox(height: 12),
          Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              _ChartLegend(
                color: positiveColor(context),
                label: '상승 · 채운 몸통',
                filled: true,
              ),
              _ChartLegend(
                color: Theme.of(context).colorScheme.error,
                label: '하락 · 빈 몸통',
                filled: false,
              ),
              const _ChartLegend(
                color: Colors.grey,
                label: '아래 막대 · 거래량',
                filled: true,
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _ChartLegend extends StatelessWidget {
  const _ChartLegend({
    required this.color,
    required this.label,
    required this.filled,
  });
  final Color color;
  final String label;
  final bool filled;
  @override
  Widget build(BuildContext context) => Row(
    children: [
      Container(
        width: 14,
        height: 14,
        decoration: BoxDecoration(
          color: filled ? color : Colors.transparent,
          border: Border.all(color: color),
        ),
      ),
      const SizedBox(width: 6),
      Expanded(child: Text(label)),
    ],
  );
}

class _CandlePainter extends CustomPainter {
  const _CandlePainter({
    required this.bars,
    required this.bull,
    required this.bear,
    required this.axis,
  });
  final List<MarketBar> bars;
  final Color bull;
  final Color bear;
  final Color axis;

  @override
  void paint(Canvas canvas, Size size) {
    const padding = 10.0;
    final priceHeight = size.height * 0.72;
    double geometry(String value) {
      final parsed = double.tryParse(value);
      return parsed != null && parsed.isFinite ? parsed : 0;
    }

    final values = bars
        .expand((bar) => [bar.low, bar.high])
        .map(geometry)
        .toList();
    final min = values.reduce((a, b) => a < b ? a : b);
    final max = values.reduce((a, b) => a > b ? a : b);
    final flat = max == min;
    final range = flat ? 1.0 : max - min;
    final maxVolume = bars
        .map((bar) => geometry(bar.volume))
        .reduce((a, b) => a > b ? a : b);
    final spacing = (size.width - padding * 2) / bars.length;
    final width = (spacing * 0.62).clamp(2.0, 16.0);
    final axisPaint = Paint()
      ..color = axis
      ..strokeWidth = 1;
    canvas.drawLine(
      Offset(padding, priceHeight),
      Offset(size.width - padding, priceHeight),
      axisPaint,
    );
    for (var index = 0; index < bars.length; index++) {
      final bar = bars[index];
      final x = padding + spacing * (index + 0.5);
      double y(String value) => flat
          ? padding + (priceHeight - padding * 2) / 2
          : padding +
                (max - geometry(value)) / range * (priceHeight - padding * 2);
      final open = y(bar.open);
      final close = y(bar.close);
      final rising = geometry(bar.close) >= geometry(bar.open);
      final color = rising ? bull : bear;
      final paint = Paint()
        ..color = color
        ..strokeWidth = 1.5
        ..style = PaintingStyle.stroke;
      canvas.drawLine(Offset(x, y(bar.high)), Offset(x, y(bar.low)), paint);
      final body = Rect.fromLTRB(
        x - width / 2,
        open < close ? open : close,
        x + width / 2,
        open < close ? close : open,
      );
      if (rising) {
        paint.style = PaintingStyle.fill;
      }
      canvas.drawRect(
        body.height < 1
            ? Rect.fromLTWH(body.left, body.top, body.width, 1)
            : body,
        paint,
      );
      final volumeHeight = maxVolume == 0
          ? 0.0
          : geometry(bar.volume) /
                maxVolume *
                (size.height - priceHeight - padding);
      canvas.drawRect(
        Rect.fromLTWH(
          x - width / 2,
          size.height - volumeHeight,
          width,
          volumeHeight,
        ),
        Paint()..color = color.withValues(alpha: 0.55),
      );
    }
  }

  @override
  bool shouldRepaint(covariant _CandlePainter oldDelegate) =>
      oldDelegate.bars != bars ||
      oldDelegate.bull != bull ||
      oldDelegate.bear != bear ||
      oldDelegate.axis != axis;
}

class _OhlcvTable extends StatelessWidget {
  const _OhlcvTable({required this.bars});
  final List<MarketBar> bars;

  @override
  Widget build(BuildContext context) => SectionCard(
    title: 'OHLCV 표',
    child: SizedBox(
      height: 360,
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: SizedBox(
          width: 1320,
          child: Column(
            children: [
              const _OhlcvRow.header(),
              const Divider(height: 1),
              Expanded(
                child: ListView.builder(
                  key: const Key('ohlcv-table-rows'),
                  itemExtent: 56,
                  itemCount: bars.length,
                  itemBuilder: (context, index) => _OhlcvRow(bar: bars[index]),
                ),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}

class _OhlcvRow extends StatelessWidget {
  const _OhlcvRow.header() : bar = null;
  const _OhlcvRow({required this.bar});
  final MarketBar? bar;

  @override
  Widget build(BuildContext context) {
    final value = bar;
    if (value == null) {
      return const Row(
        children: [
          _TableCell(
            '시각',
            width: 420,
            header: true,
            semanticKey: Key('ohlcv-header-at'),
          ),
          _TableCell('시가', width: 180, header: true),
          _TableCell('고가', width: 180, header: true),
          _TableCell('저가', width: 180, header: true),
          _TableCell('종가', width: 180, header: true),
          _TableCell('거래량', width: 180, header: true),
        ],
      );
    }
    return Row(
      children: [
        _TableCell(
          value.at,
          width: 420,
          semanticLabel: '행 ${value.at}, 시각 ${value.at}',
        ),
        _TableCell(
          value.open,
          width: 180,
          semanticLabel: '행 ${value.at}, 시가 ${value.open}',
        ),
        _TableCell(
          value.high,
          width: 180,
          semanticLabel: '행 ${value.at}, 고가 ${value.high}',
        ),
        _TableCell(
          value.low,
          width: 180,
          semanticLabel: '행 ${value.at}, 저가 ${value.low}',
        ),
        _TableCell(
          value.close,
          width: 180,
          semanticLabel: '행 ${value.at}, 종가 ${value.close}',
        ),
        _TableCell(
          value.volume,
          width: 180,
          semanticLabel: '행 ${value.at}, 거래량 ${value.volume}',
        ),
      ],
    );
  }
}

class _TableCell extends StatelessWidget {
  const _TableCell(
    this.value, {
    required this.width,
    this.semanticLabel,
    this.header = false,
    this.semanticKey,
  });
  final String value;
  final double width;
  final String? semanticLabel;
  final bool header;
  final Key? semanticKey;
  @override
  Widget build(BuildContext context) => Semantics(
    key: semanticKey,
    container: true,
    excludeSemantics: true,
    header: header,
    label: semanticLabel ?? value,
    child: SizedBox(
      width: width,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
        child: Text(value, style: tabular(context)),
      ),
    ),
  );
}
