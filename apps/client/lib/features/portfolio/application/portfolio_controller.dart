import 'dart:async';
import 'package:omni_folio_client/core/domain/state_signal.dart';
import 'package:omni_folio_client/features/connections/domain/connections_repository.dart';
import 'package:omni_folio_client/features/connections/domain/models.dart';
import 'package:omni_folio_client/features/portfolio/domain/models.dart';
import 'package:omni_folio_client/features/portfolio/domain/portfolio_repository.dart';

class PortfolioController extends StateSignal {
  PortfolioController(this.api, {required this.connections});
  final ConnectionsRepository connections;
  final PortfolioRepository api;
  ServiceStatus? status;
  PortfolioSnapshot? snapshot;
  CurrentPortfolioValuation? currentValuation;
  BrokerReconciliation? reconciliation;
  LocalOrderLog? orderLog;
  String? error;
  var reconciliationFailed = false;
  var currentValuationFailed = false;
  var orderLogFailed = false;
  var busy = false;
  var reconciliationBusy = false;
  var currentValuationBusy = false;
  var orderLogBusy = false;
  var _disposed = false;
  var _marketDataRefreshPending = false;

  void refreshAfterMarketDataImport() {
    _marketDataRefreshPending = true;
    _drainMarketDataRefresh();
  }

  void _drainMarketDataRefresh() {
    if (_disposed ||
        !_marketDataRefreshPending ||
        busy ||
        currentValuationBusy) {
      return;
    }
    _marketDataRefreshPending = false;
    unawaited(refresh());
  }

  Future<void> refresh() async {
    if (busy) return;
    busy = true;
    error = null;
    _notify();
    final optionalRefreshes = <Future<void>>[
      if (!currentValuationBusy) refreshCurrentValuation(),
      if (!reconciliationBusy) refreshReconciliation(),
      if (!orderLogBusy) refreshLocalOrders(),
    ];
    final results = await Future.wait([
      _settle(connections.status()),
      _settle(api.snapshot()),
    ]);
    final statusResult = results[0] as _Result<ServiceStatus>;
    final snapshotResult = results[1] as _Result<PortfolioSnapshot>;
    if (statusResult.value != null) status = statusResult.value;
    if (snapshotResult.value != null) snapshot = snapshotResult.value;
    error = statusResult.error ?? snapshotResult.error;
    busy = false;
    _notify();
    _drainMarketDataRefresh();
    await Future.wait(optionalRefreshes);
    _drainMarketDataRefresh();
  }

  Future<void> refreshCurrentValuation() async {
    if (currentValuationBusy) return;
    currentValuationBusy = true;
    currentValuationFailed = false;
    _notify();
    try {
      currentValuation = await api.currentPortfolioValuation();
    } catch (_) {
      currentValuationFailed = true;
    } finally {
      currentValuationBusy = false;
      _notify();
      _drainMarketDataRefresh();
    }
  }

  Future<void> refreshReconciliation() async {
    if (reconciliationBusy) return;
    reconciliationBusy = true;
    reconciliationFailed = false;
    _notify();
    try {
      reconciliation = await connections.latestBrokerReconciliation();
    } catch (_) {
      reconciliationFailed = true;
    } finally {
      reconciliationBusy = false;
      _notify();
    }
  }

  Future<void> refreshLocalOrders() async {
    if (orderLogBusy) return;
    orderLogBusy = true;
    orderLogFailed = false;
    _notify();
    try {
      orderLog = await connections.localOrders();
    } catch (_) {
      orderLogFailed = true;
    } finally {
      orderLogBusy = false;
      _notify();
    }
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }

  DataState get state {
    if (busy && status == null && snapshot == null) return DataState.loading;
    if (error != null && status == null && snapshot == null) {
      return DataState.error;
    }
    if (error != null ||
        status?.trustState == 'partial' ||
        status?.trustState == 'error') {
      return DataState.partial;
    }
    if (status?.trustState == 'never_verified') {
      return DataState.neverVerified;
    }
    if (status?.trustState == 'stale') return DataState.stale;
    if (status == null && snapshot == null) return DataState.empty;
    return DataState.success;
  }
}

enum DataState { loading, empty, error, partial, neverVerified, stale, success }

class _Result<T> {
  const _Result.value(this.value) : error = null;
  const _Result.error(this.error) : value = null;
  final T? value;
  final String? error;
}

Future<_Result<T>> _settle<T>(Future<T> future) async {
  try {
    return _Result.value(await future);
  } catch (_) {
    return const _Result.error('데이터를 불러오지 못했습니다. 다시 시도하세요.');
  }
}
