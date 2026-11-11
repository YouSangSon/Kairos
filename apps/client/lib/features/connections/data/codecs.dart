import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/core/domain/decimal.dart';
import 'package:omni_folio_client/features/connections/domain/models.dart';

ServiceStatus decodeServiceStatus(Json json) {
  final liveEnabled = jsonBool(json, 'live_enabled');
  final mode = jsonText(json, 'mode');
  final trustState = jsonText(json, 'trust_state');
  if (liveEnabled || mode != 'local_import_only') {
    throw const FormatException('Unsupported service mode');
  }
  if (!const {
    'never_verified',
    'verified',
    'stale',
    'partial',
    'error',
  }.contains(trustState)) {
    throw const FormatException('Unsupported trust state');
  }
  final lastVerifiedAt = jsonOptionalDate(json, 'last_verified_at');
  final issues = jsonJsonList(
    json,
    'issues',
  ).map((item) => jsonText(item, 'message')).toList(growable: false);
  if ((trustState == 'verified' && lastVerifiedAt == null) ||
      (trustState == 'never_verified' && lastVerifiedAt != null)) {
    throw const FormatException('Inconsistent verification state');
  }
  return ServiceStatus(
    liveEnabled: liveEnabled,
    mode: mode,
    trustState: trustState,
    ledgerRevision: jsonText(json, 'ledger_revision'),
    lastVerifiedAt: lastVerifiedAt,
    issues: issues,
  );
}

LocalOrderView decodeLocalOrderView(Json json) {
  jsonRequireExactKeys(json, const {
    'mode',
    'symbol',
    'side',
    'order_type',
    'quantity',
    'limit_price',
    'filled_quantity',
    'currency',
    'status',
    'pending_action',
    'last_recorded_at',
  });
  final mode = jsonText(json, 'mode');
  final symbol = jsonText(json, 'symbol');
  final side = jsonText(json, 'side');
  final orderType = jsonText(json, 'order_type');
  final quantity = jsonText(json, 'quantity');
  final limitPrice = jsonDecimal(json, 'limit_price');
  final filledQuantity = jsonDecimal(json, 'filled_quantity');
  final currency = jsonText(json, 'currency');
  final status = jsonText(json, 'status');
  final pendingAction = jsonText(json, 'pending_action');
  final pendingActionMatchesStatus = switch (pendingAction) {
    'SUBMIT' => status == 'SUBMIT_UNKNOWN',
    'CANCEL' => status == 'CANCEL_UNKNOWN' || status == 'FILLED',
    'none' => status != 'SUBMIT_UNKNOWN' && status != 'CANCEL_UNKNOWN',
    _ => false,
  };
  if (!const {'synthetic', 'paper'}.contains(mode) ||
      !RegExp(r'^[0-9]{6}$').hasMatch(symbol) ||
      !const {'BUY', 'SELL'}.contains(side) ||
      orderType != 'LIMIT' ||
      !RegExp(r'^[1-9][0-9]*$').hasMatch(quantity) ||
      limitPrice == '0' ||
      limitPrice.startsWith('-') ||
      filledQuantity.startsWith('-') ||
      comparePositiveDecimal(filledQuantity, quantity) > 0 ||
      currency != 'KRW' ||
      !pendingActionMatchesStatus ||
      !const {
        'RECORDED',
        'READY',
        'RISK_REJECTED',
        'SUBMIT_UNKNOWN',
        'OPEN',
        'REJECTED',
        'PARTIALLY_FILLED',
        'CANCEL_UNKNOWN',
        'CANCELED',
        'FILLED',
      }.contains(status)) {
    throw const FormatException('Invalid local order lifecycle row');
  }
  return LocalOrderView(
    mode: mode,
    symbol: symbol,
    side: side,
    orderType: orderType,
    quantity: quantity,
    limitPrice: limitPrice,
    filledQuantity: filledQuantity,
    currency: currency,
    status: status,
    pendingAction: pendingAction,
    lastRecordedAt: jsonRfc3339Text(json['last_recorded_at']),
  );
}

LocalOrderLog decodeLocalOrderLog(Json json) {
  jsonRequireExactKeys(json, const {'source', 'broker_freshness', 'orders'});
  if (jsonText(json, 'source') != 'local_order_log' ||
      jsonText(json, 'broker_freshness') != 'unverified') {
    throw const FormatException('Invalid local order provenance');
  }
  final orders = jsonJsonList(
    json,
    'orders',
  ).map(decodeLocalOrderView).toList(growable: false);
  return LocalOrderLog(orders: orders);
}

BrokerPositionDifference decodeBrokerPositionDifference(Json json) {
  jsonRequireExactKeys(json, const {
    'symbol',
    'broker_quantity',
    'ledger_quantity',
    'difference',
    'match',
  });
  final symbol = jsonText(json, 'symbol');
  final brokerQuantity = jsonDecimal(json, 'broker_quantity');
  final ledgerQuantity = jsonDecimal(json, 'ledger_quantity');
  final difference = jsonDecimal(json, 'difference');
  final match = jsonBool(json, 'match');
  if (!RegExp(r'^[0-9]{6}$').hasMatch(symbol) ||
      brokerQuantity.startsWith('-') ||
      ledgerQuantity.startsWith('-') ||
      match != (difference == '0')) {
    throw const FormatException('Invalid broker reconciliation row');
  }
  return BrokerPositionDifference(
    symbol: symbol,
    brokerQuantity: brokerQuantity,
    ledgerQuantity: ledgerQuantity,
    difference: difference,
    match: match,
  );
}

BrokerReconciliation decodeBrokerReconciliation(Json json) {
  jsonRequireExactKeys(json, const {
    'provider',
    'environment',
    'exchange',
    'freshness',
    'fetched_at',
    'recorded_at',
    'ledger_revision',
    'all_positions_match',
    'position_differences',
  });
  final provider = jsonText(json, 'provider');
  final environment = jsonText(json, 'environment');
  final exchange = jsonText(json, 'exchange');
  final freshness = jsonText(json, 'freshness');
  final ledgerRevision = jsonText(json, 'ledger_revision');
  final allPositionsMatch = jsonBool(json, 'all_positions_match');
  final differences = jsonJsonList(
    json,
    'position_differences',
  ).map(decodeBrokerPositionDifference).toList(growable: false);
  var previousSymbol = '';
  for (final difference in differences) {
    if (difference.symbol.compareTo(previousSymbol) <= 0) {
      throw const FormatException(
        'Broker reconciliation symbols must be ordered',
      );
    }
    previousSymbol = difference.symbol;
  }
  if (provider != 'kiwoom' ||
      !const {'mock', 'production'}.contains(environment) ||
      exchange != 'KRX' ||
      freshness != 'unverified' ||
      !RegExp(r'^rev_[0-9]{10}$').hasMatch(ledgerRevision) ||
      allPositionsMatch != differences.every((item) => item.match)) {
    throw const FormatException('Invalid broker reconciliation');
  }
  return BrokerReconciliation(
    provider: provider,
    environment: environment,
    exchange: exchange,
    freshness: freshness,
    fetchedAt: jsonRfc3339Text(json['fetched_at']),
    recordedAt: jsonRfc3339Text(json['recorded_at']),
    ledgerRevision: ledgerRevision,
    allPositionsMatch: allPositionsMatch,
    positionDifferences: differences,
  );
}
