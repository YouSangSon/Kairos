import 'dart:async';
import 'package:flutter/material.dart';
import 'package:omni_folio_client/core/presentation/components.dart';
import 'package:omni_folio_client/features/ledger/application/import_apply_controller.dart';
import 'package:omni_folio_client/features/ledger/data/csv_file.dart';
import 'package:omni_folio_client/features/ledger/domain/ledger_repository.dart';
import 'package:omni_folio_client/features/ledger/domain/models.dart';
import 'package:omni_folio_client/features/ledger/presentation/manual_entry_page.dart';

class ActivityPage extends StatefulWidget {
  const ActivityPage({super.key, required this.api, this.applies});
  final LedgerRepository api;
  final ImportApplyController? applies;

  @override
  State<ActivityPage> createState() => _ActivityPageState();
}

class _ActivityPageState extends State<ActivityPage> {
  final _csv = TextEditingController();
  final _scroll = ScrollController();
  final _activityHistoryKey = GlobalKey();
  ImportPreview? _preview;
  ApplyReceipt? _receipt;
  ApplyReceipt? _previousReceipt;
  late final ImportApplyController _applies;
  late final bool _ownsApplies;
  late int _seenCompletion;
  late int _seenRejection;
  LedgerActivityPage? _firstActivities;
  LedgerActivityPage? _activities;
  final List<({String cursor, String upperBoundOccurredAt})>
  _activityCursorTrail = [];
  String? _error;
  var _previewBusy = false;
  var _manualOpen = false;
  var _fileBusy = false;
  var _activityBusy = false;
  var _activityFailed = false;
  var _activityPagingFailed = false;
  var _activityPageIndex = 0;
  var _activityEpoch = 0;
  int? _queuedFirstPageEpoch;
  var _queuedFirstPageReveal = false;

  bool get _busy => _previewBusy || _applies.busy;

  @override
  void initState() {
    super.initState();
    _ownsApplies = widget.applies == null;
    _applies = widget.applies ?? ImportApplyController(widget.api);
    _seenCompletion = _applies.completionSerial;
    _seenRejection = _applies.rejectionSerial;
    _previousReceipt = _applies.completion?.receipt;
    _applies.addListener(_applyStateChanged);
    _refreshActivities(revealResult: false);
  }

  @override
  void dispose() {
    _activityEpoch += 1;
    _applies.removeListener(_applyStateChanged);
    if (_ownsApplies) _applies.dispose();
    _csv.dispose();
    _scroll.dispose();
    super.dispose();
  }

  void _applyStateChanged() {
    if (!mounted) return;
    var refresh = false;
    setState(() {
      if (_seenCompletion != _applies.completionSerial) {
        _seenCompletion = _applies.completionSerial;
        final completed = _applies.completion!;
        if (completed.csv == _csv.text) {
          _receipt = completed.receipt;
          _previousReceipt = null;
          if (_preview?.previewId != completed.previewId) _preview = null;
        } else {
          _previousReceipt = completed.receipt;
          _receipt = null;
          _preview = null;
          _error = '이전 입력의 반영이 완료됐습니다. 현재 CSV는 새로 미리보기를 만드세요.';
        }
        refresh = true;
      }
      if (_seenRejection != _applies.rejectionSerial) {
        _seenRejection = _applies.rejectionSerial;
        _preview = null;
        _receipt = null;
      }
    });
    if (refresh) unawaited(_refreshActivities(revealResult: false));
  }

  Future<void> _openManualEntry() async {
    if (_busy || _fileBusy || _manualOpen || _applies.pending != null) return;
    setState(() => _manualOpen = true);
    try {
      final csv = await Navigator.of(context).push<String>(
        MaterialPageRoute(builder: (_) => const ManualEntryPage()),
      );
      if (!mounted || csv == null || csv == _csv.text) return;
      _csv.text = csv;
      _error = null;
      _invalidatePreviewOnEdit(csv);
    } finally {
      if (mounted) setState(() => _manualOpen = false);
    }
  }

  Future<void> _refreshActivities({bool revealResult = true}) async {
    if (!mounted) return;
    final epoch = ++_activityEpoch;
    if (_activityBusy) {
      _queuedFirstPageEpoch = epoch;
      _queuedFirstPageReveal = revealResult;
      setState(() {
        _activityFailed = false;
        _activityPagingFailed = false;
      });
      return;
    }
    await _loadActivityPage(
      epoch: epoch,
      refreshFirst: true,
      revealResult: revealResult,
    );
  }

  Future<void> _loadActivityPage({
    required int epoch,
    required bool refreshFirst,
    String? cursor,
    String? upperBoundOccurredAt,
    String? lowerBoundOccurredAt,
    int targetPageIndex = 0,
    bool movingOlder = false,
    bool revealResult = true,
  }) async {
    if (!mounted || _activityBusy) return;
    setState(() {
      _activityBusy = true;
      _activityFailed = false;
      _activityPagingFailed = false;
    });
    try {
      final activities = await widget.api.ledgerActivities(cursor: cursor);
      if (!mounted || epoch != _activityEpoch) return;
      if (cursor != null) {
        final first = _firstActivities;
        if (first == null || upperBoundOccurredAt == null) {
          throw const FormatException('Missing activity page snapshot');
        }
        activities.requireContinuationOf(
          initial: first,
          upperBoundOccurredAt: upperBoundOccurredAt,
          requestedCursor: cursor,
          lowerBoundOccurredAt: lowerBoundOccurredAt,
          traversedCursors: _activityCursorTrail
              .take(targetPageIndex - 1)
              .map((step) => step.cursor),
        );
      }
      setState(() {
        if (refreshFirst) {
          _firstActivities = activities;
          _activities = activities;
          _activityPageIndex = 0;
          _activityCursorTrail.clear();
        } else {
          _activities = activities;
          _activityPageIndex = targetPageIndex;
          if (movingOlder) {
            _activityCursorTrail.removeRange(
              targetPageIndex - 1,
              _activityCursorTrail.length,
            );
            _activityCursorTrail.add((
              cursor: cursor!,
              upperBoundOccurredAt: upperBoundOccurredAt!,
            ));
          } else if (_activityCursorTrail.length > targetPageIndex) {
            _activityCursorTrail.removeRange(
              targetPageIndex,
              _activityCursorTrail.length,
            );
          }
        }
        _activityFailed = false;
        _activityPagingFailed = false;
      });
      if (revealResult) _revealActivityHistory();
    } catch (_) {
      if (mounted && epoch == _activityEpoch) {
        setState(() {
          _activityFailed = true;
          _activityPagingFailed = cursor != null;
        });
      }
    } finally {
      if (mounted) {
        setState(() => _activityBusy = false);
        final queuedEpoch = _queuedFirstPageEpoch;
        if (queuedEpoch != null) {
          final revealQueuedResult = _queuedFirstPageReveal;
          _queuedFirstPageEpoch = null;
          _queuedFirstPageReveal = false;
          await _loadActivityPage(
            epoch: queuedEpoch,
            refreshFirst: true,
            revealResult: revealQueuedResult,
          );
        }
      }
    }
  }

  void _revealActivityHistory() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final historyContext = _activityHistoryKey.currentContext;
      if (!mounted || historyContext == null || !_scroll.hasClients) return;
      Scrollable.ensureVisible(
        historyContext,
        duration: Duration.zero,
        alignment: 0,
      );
    });
  }

  Future<void> _showOlderActivities() async {
    final activities = _activities;
    final cursor = activities?.nextCursor;
    if (_activityBusy || activities == null || cursor == null) return;
    await _loadActivityPage(
      epoch: _activityEpoch,
      refreshFirst: false,
      cursor: cursor,
      upperBoundOccurredAt: activities.events.last.occurredAt,
      targetPageIndex: _activityPageIndex + 1,
      movingOlder: true,
    );
  }

  Future<void> _showNewerActivities() async {
    final activities = _activities;
    if (_activityBusy || _activityPageIndex == 0 || activities == null) return;
    if (_activityPageIndex == 1) {
      setState(() {
        _activities = _firstActivities;
        _activityPageIndex = 0;
        _activityCursorTrail.clear();
        _activityFailed = false;
        _activityPagingFailed = false;
      });
      _revealActivityHistory();
      return;
    }
    final targetPageIndex = _activityPageIndex - 1;
    final step = _activityCursorTrail[targetPageIndex - 1];
    await _loadActivityPage(
      epoch: _activityEpoch,
      refreshFirst: false,
      cursor: step.cursor,
      upperBoundOccurredAt: step.upperBoundOccurredAt,
      lowerBoundOccurredAt: activities.events.first.occurredAt,
      targetPageIndex: targetPageIndex,
    );
  }

  Future<void> _selectCsvFile() async {
    if (_busy || _fileBusy) return;
    setState(() => _fileBusy = true);
    try {
      final csv = await selectCsvFile();
      if (!mounted || csv == null || csv == _csv.text) return;
      _csv.text = csv;
      _error = null;
      _invalidatePreviewOnEdit(csv);
    } on CsvFileException catch (error) {
      if (mounted) setState(() => _error = error.message);
    } catch (_) {
      if (mounted) {
        setState(
          () => _error =
              'CSV 파일을 읽지 못했습니다. 다시 선택하거나 내용을 붙여 넣으세요. 기존 입력과 결과는 유지됩니다.',
        );
      }
    } finally {
      if (mounted) setState(() => _fileBusy = false);
    }
  }

  Future<void> _previewCsv() async {
    if (_busy || _fileBusy) return;
    if (_csv.text.trim().isEmpty) {
      setState(() => _error = 'CSV 내용을 붙여 넣으세요. 미리보기는 원장을 변경하지 않습니다.');
      return;
    }
    final csv = _csv.text;
    setState(() {
      _previewBusy = true;
      _error = null;
      _receipt = null;
    });
    try {
      final preview = await widget.api.preview(csv);
      if (mounted && _csv.text == csv) {
        setState(() => _preview = preview);
      } else if (mounted) {
        setState(() => _error = 'CSV 내용이 변경되어 이전 미리보기는 무효입니다. 새 미리보기를 만드세요.');
      }
    } catch (_) {
      if (mounted && _csv.text == csv) {
        setState(() {
          _error = '미리보기를 완료하지 못했습니다. 입력을 확인한 뒤 다시 시도하세요.';
          if (_preview != null) _error = '$_error\n이전 미리보기는 유지됩니다.';
        });
      }
    } finally {
      if (mounted) setState(() => _previewBusy = false);
    }
  }

  Future<void> _apply() async {
    if (_busy || _fileBusy) return;
    final preview = _preview;
    if (preview == null || !preview.canApply) return;
    setState(() => _error = null);
    await _applies.apply(preview, _csv.text);
  }

  void _invalidatePreviewOnEdit(String _) {
    if (_preview == null && _receipt == null) {
      setState(() {});
      return;
    }
    setState(() {
      _preview = null;
      _receipt = null;
      _error = 'CSV 내용이 변경되어 이전 미리보기는 무효입니다. 새 미리보기를 만드세요.';
    });
  }

  @override
  Widget build(BuildContext context) => ListView(
    controller: _scroll,
    padding: const EdgeInsets.all(16),
    children: [
      SectionCard(
        title: '거래 내역 가져오기',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('UTF-8·최대 1MiB CSV를 선택하거나 붙여 넣으세요. 선택만으로 전송되지 않습니다.'),
            const SizedBox(height: 12),
            TextField(
              controller: _csv,
              readOnly: _fileBusy,
              onChanged: _invalidatePreviewOnEdit,
              minLines: 6,
              maxLines: 12,
              keyboardType: TextInputType.multiline,
              decoration: const InputDecoration(
                labelText: 'CSV 텍스트',
                hintText: 'header\nrow…',
              ),
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 12,
              runSpacing: 8,
              children: [
                OutlinedButton.icon(
                  onPressed: _busy || _fileBusy ? null : _selectCsvFile,
                  icon: const Icon(Icons.file_open_outlined),
                  label: const Text('CSV 파일 선택'),
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size(0, 52),
                  ),
                ),
                OutlinedButton.icon(
                  onPressed:
                      _busy ||
                          _fileBusy ||
                          _manualOpen ||
                          _applies.pending != null
                      ? null
                      : _openManualEntry,
                  icon: const Icon(Icons.edit_note_outlined),
                  label: const Text('거래 직접 입력'),
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size(0, 52),
                  ),
                ),
                ElevatedButton.icon(
                  onPressed: _busy || _fileBusy ? null : _previewCsv,
                  icon: const Icon(Icons.preview),
                  label: const Text('미리보기 만들기'),
                  style: ElevatedButton.styleFrom(
                    minimumSize: const Size(0, 52),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
      if (_busy || _fileBusy)
        const Padding(
          padding: EdgeInsets.all(16),
          child: Loading(compact: true),
        ),
      if (_error != null)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: Semantics(
            key: const Key('import-error'),
            container: true,
            liveRegion: true,
            child: Notice(
              icon: Icons.error_outline,
              text: _error!,
              color: Theme.of(context).colorScheme.error,
            ),
          ),
        ),
      if (_applies.pending != null)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Semantics(
                container: true,
                liveRegion: true,
                child: Notice(
                  icon: Icons.history,
                  color: _applies.busy
                      ? Theme.of(context).colorScheme.primary
                      : Theme.of(context).colorScheme.error,
                  text: _applies.busy
                      ? _applies.pending!.csv == _csv.text
                            ? '이 입력의 반영 요청을 처리하고 있습니다.'
                            : '이전 입력의 반영 요청을 처리하고 있습니다. 현재 CSV의 결과가 아닙니다.'
                      : _applies.pending!.csv == _csv.text
                      ? '거래 내역의 반영 여부를 확인하지 못했습니다. 같은 요청을 다시 시도해도 중복 반영되지 않습니다.'
                      : '이전 CSV의 반영 여부를 확인하지 못했습니다. 현재 입력은 보존합니다. 같은 거래를 새로 작성하지 말고 이전 요청을 재시도하세요.',
                ),
              ),
              if (!_applies.busy) ...[
                const SizedBox(height: 8),
                const Text('반영 여부를 확인할 때까지 앱을 닫거나 같은 거래를 새로 작성하지 마세요.'),
              ],
              if (_preview?.canApply != true ||
                  _applies.pending!.csv != _csv.text) ...[
                const SizedBox(height: 8),
                const Text('재시도는 원래 요청을 사용하며, 아직 반영되지 않았다면 원장을 변경할 수 있습니다.'),
                const SizedBox(height: 8),
                ElevatedButton(
                  onPressed: _busy || _fileBusy ? null : _applies.retry,
                  child: const Text('이전 요청 재시도'),
                ),
              ],
            ],
          ),
        ),
      if (_applies.failure == ImportApplyFailure.rejected)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: Notice(
            icon: Icons.error_outline,
            text: '이전 반영 요청이 거절되었습니다. 입력을 확인해 새 미리보기를 만드세요.',
            color: Theme.of(context).colorScheme.error,
          ),
        ),
      if (_preview != null)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: _PreviewCard(
            preview: _preview!,
            onApply:
                _busy ||
                    _fileBusy ||
                    (_applies.pending != null &&
                        _applies.pending!.csv != _csv.text)
                ? null
                : _apply,
          ),
        ),
      if (_receipt != null)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: _ReceiptCard(receipt: _receipt!),
        ),
      if (_previousReceipt != null)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: _ReceiptCard(receipt: _previousReceipt!, previousInput: true),
        ),
      if (_receipt != null ||
          _previousReceipt != null ||
          (_csv.text.isEmpty && _preview == null && _error == null)) ...[
        const SizedBox(height: 12),
        _ActivityHistoryCard(
          key: _activityHistoryKey,
          page: _activities,
          busy: _activityBusy,
          failed: _activityFailed,
          pagingFailed: _activityPagingFailed,
          pageIndex: _activityPageIndex,
          onRetry: () => _refreshActivities(),
          onOlder: _showOlderActivities,
          onNewer: _showNewerActivities,
        ),
      ],
    ],
  );
}

class _ActivityHistoryCard extends StatelessWidget {
  const _ActivityHistoryCard({
    super.key,
    required this.page,
    required this.busy,
    required this.failed,
    required this.pagingFailed,
    required this.pageIndex,
    required this.onRetry,
    required this.onOlder,
    required this.onNewer,
  });

  final LedgerActivityPage? page;
  final bool busy;
  final bool failed;
  final bool pagingFailed;
  final int pageIndex;
  final VoidCallback onRetry;
  final VoidCallback onOlder;
  final VoidCallback onNewer;

  @override
  Widget build(BuildContext context) {
    final value = page;
    return SectionCard(
      title: '최근 거래',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (value == null && busy) const Loading(compact: true),
          if (value == null && failed) ...[
            Semantics(
              liveRegion: true,
              child: Notice(
                icon: Icons.error_outline,
                text: '거래 내역을 불러오지 못했습니다. 다시 시도하세요.',
                color: Theme.of(context).colorScheme.error,
              ),
            ),
            const SizedBox(height: 12),
            OutlinedButton(
              onPressed: onRetry,
              child: const Text('최근 거래 다시 불러오기'),
            ),
          ],
          if (value != null) ...[
            const Text('로컬 원장 기록 · 현재 증권사 상태 아님'),
            const SizedBox(height: 4),
            Text(
              '기록 ${time(DateTime.parse(value.recordedAt))} · 원장 ${value.ledgerRevision}',
              style: tabular(context),
            ),
            if (busy) ...[
              const SizedBox(height: 8),
              Semantics(
                liveRegion: true,
                child: Notice(
                  icon: Icons.sync,
                  text: '거래 내역 페이지를 불러오는 중입니다.',
                  color: Theme.of(context).colorScheme.primary,
                ),
              ),
            ],
            if (failed) ...[
              const SizedBox(height: 8),
              Semantics(
                liveRegion: true,
                child: Notice(
                  icon: Icons.error_outline,
                  text: pagingFailed
                      ? '거래 페이지를 불러오지 못해 현재 페이지를 유지합니다.'
                      : '새로고침에 실패해 마지막 정상 거래 내역을 유지합니다.',
                  color: Theme.of(context).colorScheme.error,
                ),
              ),
            ],
            const SizedBox(height: 12),
            Semantics(
              liveRegion: true,
              label: '거래 내역 페이지 ${pageIndex + 1}, ${value.events.length}건',
              child: Text(
                '페이지 ${pageIndex + 1} · ${value.events.length}건',
                style: tabular(context),
              ),
            ),
            if (pageIndex > 0 || value.nextCursor != null) ...[
              const SizedBox(height: 8),
              Text(
                '한 페이지에 최대 50건만 표시합니다.',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 8),
              if (pageIndex > 0)
                ConstrainedBox(
                  constraints: const BoxConstraints(minHeight: 48),
                  child: OutlinedButton.icon(
                    onPressed: busy ? null : onNewer,
                    icon: const Icon(Icons.arrow_upward_rounded),
                    label: const Text('더 최근 거래'),
                  ),
                ),
              if (pageIndex > 0 && value.nextCursor != null)
                const SizedBox(height: 8),
              if (value.nextCursor != null)
                ConstrainedBox(
                  constraints: const BoxConstraints(minHeight: 48),
                  child: OutlinedButton.icon(
                    onPressed: busy ? null : onOlder,
                    icon: const Icon(Icons.arrow_downward_rounded),
                    label: const Text('더 오래된 거래'),
                  ),
                ),
            ],
            const SizedBox(height: 12),
            OutlinedButton(
              onPressed: onRetry,
              child: const Text('최근 거래 다시 불러오기'),
            ),
            const SizedBox(height: 12),
            if (value.events.isEmpty)
              const Text('아직 가져온 거래가 없습니다. 아래에서 CSV 거래 내역을 가져오세요.')
            else
              for (var index = 0; index < value.events.length; index++) ...[
                if (index > 0) const Divider(height: 24),
                _ActivityRow(event: value.events[index]),
              ],
          ],
        ],
      ),
    );
  }
}

class _ActivityRow extends StatelessWidget {
  const _ActivityRow({required this.event});
  final LedgerActivity event;

  @override
  Widget build(BuildContext context) {
    final occurred = time(DateTime.parse(event.occurredAt));
    if (event.type == 'FX_EXCHANGE') {
      final sold = event.amount.substring(1);
      final summary =
          '환전 · ${event.currency} $sold 매도 → ${event.counterCurrency} ${event.counterAmount} 매수';
      return Semantics(
        container: true,
        excludeSemantics: true,
        label:
            '$occurred, 환전, ${event.currency} $sold 매도, ${event.counterCurrency} ${event.counterAmount} 매수, 현재 환율 아님',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(summary, style: const TextStyle(fontWeight: FontWeight.w700)),
            const SizedBox(height: 4),
            const Text('환율이나 현재 시세를 뜻하지 않습니다.'),
            const SizedBox(height: 4),
            Text(occurred, style: tabular(context)),
          ],
        ),
      );
    }
    final type = _activityType(event.type);
    final title = event.symbol == null ? type : '$type · ${event.symbol}';
    final details = <String>[
      if (event.quantity != null) '수량 ${event.quantity}',
      '${event.currency} ${event.amount}',
      if (event.price != null) '가격 ${event.price}',
      if (event.fee != null) '수수료 ${event.fee}',
    ];
    final semantics = [
      occurred,
      type,
      if (event.symbol != null) event.symbol!,
      ...details,
      if (event.isCorrection) '원본 보존, 반대 금액으로 상쇄',
    ].join(', ');
    return Semantics(
      container: true,
      excludeSemantics: true,
      label: semantics,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: const TextStyle(fontWeight: FontWeight.w700)),
          const SizedBox(height: 4),
          Text(details.take(2).join(' · '), style: tabular(context)),
          if (details.length > 2) ...[
            const SizedBox(height: 4),
            Text(details.skip(2).join(' · '), style: tabular(context)),
          ],
          if (event.isCorrection) ...[
            const SizedBox(height: 4),
            const Text('원본 기록은 보존되고 반대 금액으로 상쇄됩니다.'),
          ],
          const SizedBox(height: 4),
          Text(occurred, style: tabular(context)),
        ],
      ),
    );
  }
}

String _activityType(String type) => switch (type) {
  'BUY' => '매수',
  'SELL' => '매도',
  'DEPOSIT' => '입금',
  'WITHDRAWAL' => '출금',
  'DIVIDEND' => '배당',
  'FEE' => '수수료',
  'TAX' => '세금',
  'SPLIT' => '주식 분할',
  'CASH_VOID' => '현금 기록 취소',
  _ => type,
};

class _PreviewCard extends StatelessWidget {
  const _PreviewCard({required this.preview, required this.onApply});
  final ImportPreview preview;
  final VoidCallback? onApply;

  @override
  Widget build(BuildContext context) => SectionCard(
    title: '미리보기 결과',
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          '신규 ${preview.newRows} · 중복 ${preview.duplicateRows} · 오류 ${preview.errorRows} · 미해결 ${preview.unresolvedRows}',
          style: tabular(context),
        ),
        const SizedBox(height: 8),
        Text(
          preview.canApply
              ? '적용 가능: 이 미리보기만 원장에 원자적으로 반영됩니다.'
              : '적용 차단: 오류 또는 미해결 행을 고친 뒤 새 미리보기를 만드세요.',
        ),
        const SizedBox(height: 4),
        Text(
          '스키마 ${preview.schemaVersion} · 매핑 ${preview.mappingVersion}',
          style: Theme.of(context).textTheme.bodySmall,
        ),
        const SizedBox(height: 12),
        ...preview.rows.take(5).map((row) {
          final target = row.correctionTarget;
          if (row.transactionType == 'FX_EXCHANGE') {
            final soldAmount = row.amount!.substring(1);
            final label =
                '환전 행 ${row.rowNumber}, ${row.currency} $soldAmount 매도, ${row.counterCurrency} ${row.counterAmount} 매수';
            return Padding(
              padding: const EdgeInsets.only(bottom: 4),
              child: Semantics(
                container: true,
                excludeSemantics: true,
                label: label,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '환전 · ${row.currency} $soldAmount 매도 → ${row.counterCurrency} ${row.counterAmount} 매수',
                      style: tabular(context),
                    ),
                    const SizedBox(height: 4),
                    const Text('환율을 계산하거나 현재 시세를 뜻하지 않습니다.'),
                  ],
                ),
              ),
            );
          }
          return Padding(
            padding: const EdgeInsets.only(bottom: 4),
            child: target == null
                ? Text(
                    '행 ${row.rowNumber}: ${row.status}${row.symbol == null ? '' : ' · ${row.symbol!}'}${row.errors.isEmpty ? '' : ' · ${row.errors.join(', ')}'}',
                    style: tabular(context),
                  )
                : Semantics(
                    container: true,
                    excludeSemantics: true,
                    label:
                        '정정 행 ${row.rowNumber}, 원본 ${target.type}, ${target.currency} ${target.amount}, source ${target.sourceEventId}, 반전 ${row.currency} ${row.amount}',
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('행 ${row.rowNumber}: ${row.status} · 정정'),
                        const SizedBox(height: 4),
                        const Text('원본 기록은 보존되고 반대 금액으로 상쇄됩니다.'),
                        const SizedBox(height: 4),
                        Text(
                          '원본 ${target.type} · ${target.currency} ${target.amount} · source ${target.sourceEventId}',
                          style: tabular(context),
                        ),
                        Text(
                          '정정 ${row.transactionType} · ${row.currency} ${row.amount}',
                          style: tabular(context),
                        ),
                      ],
                    ),
                  ),
          );
        }),
        const SizedBox(height: 8),
        ElevatedButton.icon(
          onPressed: preview.canApply ? onApply : null,
          icon: const Icon(Icons.check_circle_outline),
          label: const Text('원자적으로 적용'),
        ),
      ],
    ),
  );
}

class _ReceiptCard extends StatelessWidget {
  const _ReceiptCard({required this.receipt, this.previousInput = false});
  final ApplyReceipt receipt;
  final bool previousInput;

  @override
  Widget build(BuildContext context) => Semantics(
    liveRegion: true,
    label:
        '${previousInput ? '이전 입력 적용 확인' : '적용 확인'} receipt ${receipt.receiptId}',
    child: SectionCard(
      title: previousInput ? '이전 입력 적용 확인' : '적용 확인',
      child: Text(
        '${previousInput ? '이전 요청의 결과이며 현재 CSV의 반영 결과가 아닙니다.\n' : ''}원자적 적용 완료\n적용 ${receipt.appliedRows}건 · 중복 제외 ${receipt.skippedDuplicateRows}건\nReceipt ${receipt.receiptId}\n원장 ${receipt.ledgerRevisionAfter} · ${time(receipt.recordedAt)}',
        style: tabular(context),
      ),
    ),
  );
}
