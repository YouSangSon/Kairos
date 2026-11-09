BigInt utcNanoseconds(String value) {
  final dot = value.indexOf('.');
  final whole = dot < 0 ? value : '${value.substring(0, dot)}Z';
  final seconds = DateTime.parse(whole).microsecondsSinceEpoch ~/ 1000000;
  final fraction = dot < 0
      ? BigInt.zero
      : BigInt.parse(
          value.substring(dot + 1, value.length - 1).padRight(9, '0'),
        );
  return BigInt.from(seconds) * BigInt.from(1000000000) + fraction;
}
