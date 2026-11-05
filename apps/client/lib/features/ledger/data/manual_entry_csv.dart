import 'package:omni_folio_client/features/ledger/domain/manual_entry.dart';

String manualEntryCsv({
  required String sourceEventId,
  required String type,
  required Map<String, String> fields,
}) {
  if (!RegExp(r'^manual-[0-9a-f]{32}$').hasMatch(sourceEventId) ||
      validateManualEntry(type, fields).isNotEmpty) {
    throw const FormatException('Invalid manual entry');
  }
  String text(String field) => (fields[field] ?? '').trim();
  String number(String field, {bool signed = false}) =>
      manualDecimal(text(field), signed: signed);
  final trade = const {'BUY', 'SELL'}.contains(type);
  final symbol = const {'BUY', 'SELL', 'DIVIDEND', 'SPLIT'}.contains(type)
      ? text('symbol')
      : '';
  final quantity = trade || type == 'SPLIT' ? number('quantity') : '';
  final price = trade ? number('price') : '';
  final fee = trade ? number('fee') : '';
  final amount = manualEntryAmount(type, quantity, price, fee, text('amount'));
  final row = [
    sourceEventId,
    'account-main',
    type,
    text('occurred_at'),
    symbol,
    quantity,
    price,
    fee,
    text('currency').toUpperCase(),
    amount,
    type == 'FX_EXCHANGE' ? text('counter_currency').toUpperCase() : '',
    type == 'FX_EXCHANGE' ? number('counter_amount') : '',
    type == 'CASH_VOID' ? text('corrects_source_event_id') : '',
  ];
  String cell(String value) => '"${value.replaceAll('"', '""')}"';
  return 'source_event_id,account_id,type,occurred_at,symbol,quantity,price,fee,currency,amount,counter_currency,counter_amount,corrects_source_event_id\n'
      '${row.map(cell).join(',')}\n';
}
