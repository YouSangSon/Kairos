import 'package:omni_folio_client/core/domain/decimal.dart';

String tradeCashAmount(String type, String quantity, String price, String fee) {
  if (!const {'BUY', 'SELL'}.contains(type) ||
      [quantity, price, fee].any(
        (value) =>
            !canonicalDecimalPattern.hasMatch(value) || value.startsWith('-'),
      ) ||
      quantity == '0' ||
      price == '0') {
    throw const FormatException('Invalid trade cash input');
  }
  final productScale = decimalScale(quantity) + decimalScale(price);
  final scale = [
    productScale,
    decimalScale(fee),
  ].reduce((a, b) => a > b ? a : b);
  final gross =
      scaledDecimal(quantity, decimalScale(quantity)) *
      scaledDecimal(price, decimalScale(price)) *
      BigInt.from(10).pow(scale - productScale);
  final expected = (type == 'BUY' ? -gross : gross) - scaledDecimal(fee, scale);
  if (expected == BigInt.zero) return '0';
  final digits = expected.abs().toString().padLeft(scale + 1, '0');
  var value = digits;
  if (scale > 0) {
    final whole = digits.substring(0, digits.length - scale);
    final fraction = digits
        .substring(digits.length - scale)
        .replaceFirst(RegExp(r'0+$'), '');
    value = fraction.isEmpty ? whole : '$whole.$fraction';
  }
  return expected.isNegative ? '-$value' : value;
}
