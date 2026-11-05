import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/core/domain/decimal.dart';
import 'package:omni_folio_client/features/ledger/domain/models.dart';
import 'package:omni_folio_client/features/ledger/domain/trade_cash.dart';

LedgerActivityPage decodeLedgerActivityPage(Json json) {
  jsonRequireExactKeys(json, const {
    'source',
    'broker_freshness',
    'ledger_revision',
    'recorded_at',
    'events',
    'next_cursor',
  });
  final source = jsonText(json, 'source');
  final freshness = jsonText(json, 'broker_freshness');
  final ledgerRevision = jsonText(json, 'ledger_revision');
  final nextCursor = jsonNullableText(json, 'next_cursor');
  if (source != 'local_ledger' ||
      freshness != 'unverified' ||
      !RegExp(r'^rev_[0-9]{10}$').hasMatch(ledgerRevision) ||
      (nextCursor != null && nextCursor.length > 1024)) {
    throw const FormatException('Invalid ledger activity metadata');
  }
  final events = jsonJsonList(
    json,
    'events',
  ).map(decodeLedgerActivity).toList(growable: false);
  if (events.length > 50 || (nextCursor != null && events.length != 50)) {
    throw const FormatException('Invalid ledger activity page size');
  }
  for (var index = 1; index < events.length; index += 1) {
    if (jsonUtcNanoseconds(
          events[index].occurredAt,
        ).compareTo(jsonUtcNanoseconds(events[index - 1].occurredAt)) >
        0) {
      throw const FormatException('Ledger activities must be newest first');
    }
  }
  return LedgerActivityPage(
    source: source,
    brokerFreshness: freshness,
    ledgerRevision: ledgerRevision,
    recordedAt: jsonUtcText(json, 'recorded_at'),
    events: events,
    nextCursor: nextCursor,
  );
}

InstrumentLedger decodeInstrumentLedger(Json json) {
  jsonRequireExactKeys(json, const {
    'instrument_id',
    'symbol',
    'currency',
    'source',
    'broker_freshness',
    'ledger_revision',
    'ledger_as_of',
    'ledger_recorded_at',
    'cost_basis_policy',
    'average_cost_policy',
    'quantity',
    'cost_basis',
    'average_cost',
    'trade_limit',
    'has_more',
    'trades',
  });
  final instrumentId = jsonText(json, 'instrument_id');
  final symbol = jsonText(json, 'symbol');
  final currency = jsonCurrency(json, 'currency');
  final revision = jsonText(json, 'ledger_revision');
  final asOf = jsonUtcText(json, 'ledger_as_of');
  final recordedAt = jsonUtcText(json, 'ledger_recorded_at');
  final quantity = jsonDecimal(json, 'quantity');
  final cost = jsonDecimal(json, 'cost_basis');
  final average = jsonNullableDecimal(json, 'average_cost');
  final hasMore = jsonBool(json, 'has_more');
  final trades = jsonJsonList(
    json,
    'trades',
  ).map(decodeLedgerActivity).toList(growable: false);
  if (jsonText(json, 'source') != 'local_ledger' ||
      jsonText(json, 'broker_freshness') != 'unverified' ||
      jsonText(json, 'cost_basis_policy') !=
          'fifo_utc_nanosecond_exact_else_half_even_residual_8_v2' ||
      jsonText(json, 'average_cost_policy') !=
          'remaining_fifo_cost_per_unit_half_even_8_v1' ||
      !RegExp(r'^rev_[0-9]{10}$').hasMatch(revision) ||
      quantity.startsWith('-') ||
      cost.startsWith('-') ||
      (quantity == '0' ? (cost != '0' || average != null) : average == null) ||
      (average != null &&
          (average.startsWith('-') || decimalScale(average) > 8)) ||
      jsonCount(json, 'trade_limit') != 50 ||
      trades.isEmpty ||
      trades.length > 50 ||
      (hasMore && trades.length != 50)) {
    throw const FormatException('Invalid instrument ledger');
  }
  for (var index = 0; index < trades.length; index++) {
    final trade = trades[index];
    if (!const {'BUY', 'SELL'}.contains(trade.type) ||
        trade.currency != currency ||
        jsonUtcNanoseconds(trade.occurredAt) > jsonUtcNanoseconds(asOf) ||
        (index > 0 &&
            jsonUtcNanoseconds(trade.occurredAt) >
                jsonUtcNanoseconds(trades[index - 1].occurredAt))) {
      throw const FormatException('Invalid instrument trade scope');
    }
  }
  return InstrumentLedger(
    instrumentId: instrumentId,
    symbol: symbol,
    currency: currency,
    ledgerRevision: revision,
    ledgerAsOf: asOf,
    ledgerRecordedAt: recordedAt,
    quantity: quantity,
    costBasis: cost,
    averageCost: average,
    hasMore: hasMore,
    trades: trades,
  );
}

bool _tradeCashMatches(
  String type,
  String quantity,
  String price,
  String fee,
  String amount,
) => amount == tradeCashAmount(type, quantity, price, fee);

LedgerActivity decodeLedgerActivity(Json json) {
  jsonRequireExactKeys(json, const {
    'type',
    'occurred_at',
    'recorded_at',
    'symbol',
    'quantity',
    'price',
    'fee',
    'currency',
    'amount',
    'counter_currency',
    'counter_amount',
    'is_correction',
  });
  final type = jsonText(json, 'type');
  final symbol = jsonNullableText(json, 'symbol');
  final quantity = jsonNullableDecimal(json, 'quantity');
  final price = jsonNullableDecimal(json, 'price');
  final fee = jsonNullableDecimal(json, 'fee');
  final currency = jsonCurrency(json, 'currency');
  final amount = jsonDecimal(json, 'amount');
  final counterCurrency = jsonNullableText(json, 'counter_currency');
  final counterAmount = jsonNullableDecimal(json, 'counter_amount');
  final isCorrection = jsonBool(json, 'is_correction');
  final noTrade =
      symbol == null && quantity == null && price == null && fee == null;
  final noCounter = counterCurrency == null && counterAmount == null;
  final positive = !amount.startsWith('-') && amount != '0';
  final negative = amount.startsWith('-');
  final trade = const {'BUY', 'SELL'}.contains(type);
  var valid = isCorrection == (type == 'CASH_VOID');
  if (trade) {
    valid =
        valid &&
        symbol != null &&
        quantity != null &&
        quantity != '0' &&
        !quantity.startsWith('-') &&
        price != null &&
        price != '0' &&
        !price.startsWith('-') &&
        fee != null &&
        !fee.startsWith('-') &&
        noCounter;
    if (valid) {
      valid = _tradeCashMatches(type, quantity, price, fee, amount);
    }
  } else if (type == 'DIVIDEND') {
    valid =
        valid &&
        symbol != null &&
        quantity == null &&
        price == null &&
        fee == null &&
        noCounter &&
        positive;
  } else if (type == 'SPLIT') {
    valid =
        valid &&
        symbol != null &&
        quantity != null &&
        quantity != '0' &&
        !quantity.startsWith('-') &&
        price == null &&
        fee == null &&
        noCounter &&
        amount == '0';
  } else if (type == 'DEPOSIT') {
    valid = valid && noTrade && noCounter && positive;
  } else if (const {'WITHDRAWAL', 'FEE', 'TAX'}.contains(type)) {
    valid = valid && noTrade && noCounter && negative;
  } else if (type == 'CASH_VOID') {
    valid = valid && noTrade && noCounter && amount != '0';
  } else if (type == 'FX_EXCHANGE') {
    valid =
        valid &&
        noTrade &&
        negative &&
        counterCurrency != null &&
        RegExp(r'^[A-Z]{3}$').hasMatch(counterCurrency) &&
        counterCurrency != currency &&
        counterAmount != null &&
        counterAmount != '0' &&
        !counterAmount.startsWith('-');
  } else {
    valid = false;
  }
  if (!valid) {
    throw const FormatException('Invalid ledger activity');
  }
  return LedgerActivity(
    type: type,
    occurredAt: jsonUtcText(json, 'occurred_at'),
    recordedAt: jsonUtcText(json, 'recorded_at'),
    symbol: symbol,
    quantity: quantity,
    price: price,
    fee: fee,
    currency: currency,
    amount: amount,
    counterCurrency: counterCurrency,
    counterAmount: counterAmount,
    isCorrection: isCorrection,
  );
}

ImportPreview decodeImportPreview(Json json) {
  final totals = json['totals'];
  if (totals is! Json) throw const FormatException('Missing totals');
  return ImportPreview(
    previewId: jsonText(json, 'preview_id'),
    fileSha256: jsonText(json, 'file_sha256'),
    schemaVersion: jsonText(json, 'schema_version'),
    mappingVersion: jsonText(json, 'mapping_version'),
    ledgerRevision: jsonText(json, 'ledger_revision'),
    previewFingerprint: jsonText(json, 'preview_fingerprint'),
    canApply: jsonBool(json, 'can_apply'),
    totalRows: jsonCount(totals, 'total_rows'),
    newRows: jsonCount(totals, 'new_rows'),
    duplicateRows: jsonCount(totals, 'duplicate_rows'),
    errorRows: jsonCount(totals, 'error_rows'),
    unresolvedRows: jsonCount(totals, 'unresolved_rows'),
    rows: jsonJsonList(
      json,
      'rows',
    ).map(decodePreviewRow).toList(growable: false),
  );
}

PreviewRow decodePreviewRow(Json json) {
  final transaction = json['transaction'];
  if (transaction != null && transaction is! Json) {
    throw const FormatException('Invalid transaction');
  }
  final transactionType = transaction is Json
      ? jsonText(transaction, 'type')
      : null;
  final targetJson = json['correction_target'];
  if (targetJson != null && targetJson is! Json) {
    throw const FormatException('Invalid correction target');
  }
  final correctionTarget = targetJson is Json
      ? decodeCorrectionTarget(targetJson)
      : null;
  final counterCurrency =
      transaction is Json && transaction['counter_currency'] is String
      ? jsonText(transaction, 'counter_currency')
      : null;
  final counterAmount =
      transaction is Json && transaction['counter_amount'] is String
      ? jsonDecimal(transaction, 'counter_amount')
      : null;
  if ((counterCurrency == null) != (counterAmount == null) ||
      (transactionType == 'FX_EXCHANGE') != (counterCurrency != null)) {
    throw const FormatException(
      'FX_EXCHANGE requires one complete counter leg',
    );
  }
  final amount = transaction is Json
      ? jsonDecimal(transaction, 'amount')
      : null;
  if (transactionType == 'FX_EXCHANGE' &&
      (amount == null ||
          !amount.startsWith('-') ||
          counterAmount == '0' ||
          counterAmount!.startsWith('-') ||
          counterCurrency == jsonText(transaction as Json, 'currency'))) {
    throw const FormatException('Invalid FX_EXCHANGE cash legs');
  }
  if ((transactionType == 'CASH_VOID') != (correctionTarget != null)) {
    throw const FormatException('CASH_VOID requires one correction target');
  }
  return PreviewRow(
    rowNumber: jsonCount(json, 'row_number'),
    status: jsonText(json, 'status'),
    symbol: transaction is Json && transaction['symbol'] is String
        ? transaction['symbol'] as String
        : null,
    transactionType: transactionType,
    currency: transaction is Json ? jsonText(transaction, 'currency') : null,
    amount: amount,
    counterCurrency: counterCurrency,
    counterAmount: counterAmount,
    correctionTarget: correctionTarget,
    errors: jsonOptionalJsonList(
      json,
      'errors',
    ).map((item) => jsonText(item, 'message')).toList(growable: false),
  );
}

CorrectionTarget decodeCorrectionTarget(Json json) {
  jsonRequireExactKeys(json, const {
    'source_event_id',
    'type',
    'currency',
    'amount',
  });
  return CorrectionTarget(
    sourceEventId: jsonText(json, 'source_event_id'),
    type: jsonText(json, 'type'),
    currency: jsonText(json, 'currency'),
    amount: jsonDecimal(json, 'amount'),
  );
}

ApplyReceipt decodeApplyReceipt(Json json) => ApplyReceipt(
  receiptId: jsonText(json, 'receipt_id'),
  appliedRows: jsonCount(json, 'applied_rows'),
  skippedDuplicateRows: jsonCount(json, 'skipped_duplicate_rows'),
  ledgerRevisionAfter: jsonText(json, 'ledger_revision_after'),
  recordedAt: DateTime.parse(jsonText(json, 'recorded_at')),
);
