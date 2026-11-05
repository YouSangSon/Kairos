final canonicalDecimalPattern = RegExp(
  r'^(?:0|-?(?:[1-9][0-9]*(?:\.[0-9]*[1-9])?|0\.[0-9]*[1-9]))$',
);

String decimalPercent(String value) {
  if (!canonicalDecimalPattern.hasMatch(value)) {
    throw const FormatException('Invalid decimal percent');
  }
  final negative = value.startsWith('-');
  final parts = (negative ? value.substring(1) : value).split('.');
  final fraction = parts.length == 2 ? parts[1] : '';
  var whole = '${parts[0]}${fraction.padRight(2, '0').substring(0, 2)}';
  whole = whole.replaceFirst(RegExp(r'^0+(?=[0-9])'), '');
  final remainder = fraction.length > 2 ? fraction.substring(2) : '';
  return '${negative ? '-' : ''}$whole${remainder.isEmpty ? '' : '.$remainder'}%';
}

int decimalScale(String value) {
  final dot = value.indexOf('.');
  return dot < 0 ? 0 : value.length - dot - 1;
}

BigInt scaledDecimal(String value, int scale) {
  final ownScale = decimalScale(value);
  final coefficient = BigInt.parse(value.replaceFirst('.', ''));
  return coefficient * BigInt.from(10).pow(scale - ownScale);
}

bool decimalSumEquals(String left, String right, String total) {
  final scale = [
    left,
    right,
    total,
  ].map(decimalScale).reduce((a, b) => a > b ? a : b);
  return scaledDecimal(left, scale) + scaledDecimal(right, scale) ==
      scaledDecimal(total, scale);
}

int comparePositiveDecimal(String left, String right) {
  final leftParts = left.split('.');
  final rightParts = right.split('.');
  if (leftParts.first.length != rightParts.first.length) {
    return leftParts.first.length.compareTo(rightParts.first.length);
  }
  final integer = leftParts.first.compareTo(rightParts.first);
  if (integer != 0) return integer;
  final width = [
    leftParts.length == 1 ? 0 : leftParts.last.length,
    rightParts.length == 1 ? 0 : rightParts.last.length,
  ].reduce((a, b) => a > b ? a : b);
  final leftFraction = (leftParts.length == 1 ? '' : leftParts.last).padRight(
    width,
    '0',
  );
  final rightFraction = (rightParts.length == 1 ? '' : rightParts.last)
      .padRight(width, '0');
  return leftFraction.compareTo(rightFraction);
}
