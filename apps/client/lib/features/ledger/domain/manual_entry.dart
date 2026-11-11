import 'package:omni_folio_client/features/ledger/domain/trade_cash.dart';

const manualEntryTypeCodes = {
  'BUY',
  'SELL',
  'DEPOSIT',
  'WITHDRAWAL',
  'DIVIDEND',
  'FEE',
  'TAX',
  'SPLIT',
  'FX_EXCHANGE',
  'CASH_VOID',
};

enum ManualEntryError {
  unsupportedType,
  timestamp,
  currency,
  nonzero,
  signedDecimal,
  positiveDecimal,
  differentCurrency,
  length128,
  length64,
}

List<String> manualEntryFields(String type) => [
  'occurred_at',
  'currency',
  if (const {'BUY', 'SELL', 'DIVIDEND', 'SPLIT'}.contains(type)) 'symbol',
  if (const {'BUY', 'SELL', 'SPLIT'}.contains(type)) 'quantity',
  if (const {'BUY', 'SELL'}.contains(type)) ...['price', 'fee'],
  if (!const {'BUY', 'SELL', 'SPLIT'}.contains(type)) 'amount',
  if (type == 'FX_EXCHANGE') ...['counter_currency', 'counter_amount'],
  if (type == 'CASH_VOID') 'corrects_source_event_id',
];

Map<String, ManualEntryError> validateManualEntry(
  String type,
  Map<String, String> fields,
) {
  if (!manualEntryTypeCodes.contains(type)) {
    return {'type': ManualEntryError.unsupportedType};
  }
  final errors = <String, ManualEntryError>{};
  for (final field in manualEntryFields(type)) {
    final value = (fields[field] ?? '').trim();
    final limit = field == 'corrects_source_event_id' ? 128 : 64;
    if (value.isEmpty || value.length > limit) {
      errors[field] = limit == 128
          ? ManualEntryError.length128
          : ManualEntryError.length64;
      continue;
    }
    if (field == 'occurred_at') {
      if (!_manualTimestampValid(value)) {
        errors[field] = ManualEntryError.timestamp;
      }
    } else if (field.endsWith('currency')) {
      if (!RegExp(r'^[A-Z]{3}$').hasMatch(value.toUpperCase())) {
        errors[field] = ManualEntryError.currency;
      }
    } else if (!const {'symbol', 'corrects_source_event_id'}.contains(field)) {
      final signed = field == 'amount' && type == 'CASH_VOID';
      try {
        final number = manualDecimal(value, signed: signed);
        if (field != 'fee' && number == '0') {
          errors[field] = ManualEntryError.nonzero;
        }
      } on FormatException {
        errors[field] = signed
            ? ManualEntryError.signedDecimal
            : ManualEntryError.positiveDecimal;
      }
    }
  }
  if (type == 'FX_EXCHANGE' &&
      (fields['currency']?.trim().isNotEmpty ?? false) &&
      (fields['counter_currency']?.trim().isNotEmpty ?? false) &&
      fields['currency']?.trim().toUpperCase() ==
          fields['counter_currency']?.trim().toUpperCase()) {
    errors['counter_currency'] = ManualEntryError.differentCurrency;
  }
  return errors;
}

String manualDecimal(String value, {bool signed = false}) {
  final pattern = signed
      ? r'^-?[0-9]+(?:\.[0-9]+)?$'
      : r'^[0-9]+(?:\.[0-9]+)?$';
  if (!RegExp(pattern).hasMatch(value)) {
    throw const FormatException('Invalid manual decimal');
  }
  final negative = value.startsWith('-');
  final parts = (negative ? value.substring(1) : value).split('.');
  var whole = parts.first.replaceFirst(RegExp(r'^0+'), '');
  if (whole.isEmpty) whole = '0';
  final fraction = parts.length == 2
      ? parts.last.replaceFirst(RegExp(r'0+$'), '')
      : '';
  final canonical = fraction.isEmpty ? whole : '$whole.$fraction';
  return negative && canonical != '0' ? '-$canonical' : canonical;
}

bool _manualTimestampValid(String value) {
  final match = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|[+-](\d{2}):(\d{2}))$',
  ).firstMatch(value);
  if (match == null) return false;
  int part(int index) => int.parse(match.group(index)!);
  final year = part(1), month = part(2), day = part(3);
  final date = DateTime.utc(year, month, day);
  return date.year == year &&
      date.month == month &&
      date.day == day &&
      part(4) <= 23 &&
      part(5) <= 59 &&
      part(6) <= 59 &&
      (match.group(8) == 'Z' || (part(9) <= 23 && part(10) <= 59));
}

String manualEntryAmount(
  String type,
  String quantity,
  String price,
  String fee,
  String amount,
) {
  return const {'BUY', 'SELL'}.contains(type)
      ? tradeCashAmount(type, quantity, price, fee)
      : type == 'SPLIT'
      ? '0'
      : const {'WITHDRAWAL', 'FEE', 'TAX', 'FX_EXCHANGE'}.contains(type)
      ? '-${manualDecimal(amount)}'
      : manualDecimal(amount, signed: type == 'CASH_VOID');
}
