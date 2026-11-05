sealed class OwnerDataSummary {
  const OwnerDataSummary({
    required this.sourceVersion,
    required this.sample,
    required this.dataSha256,
    required this.pointCount,
    required this.firstObservedAt,
    required this.lastObservedAt,
    required this.fetchedAt,
  });

  final String sourceVersion, dataSha256, fetchedAt;
  final bool sample;
  final int pointCount;
  final String? firstObservedAt, lastObservedAt;
  String get kind;

  bool sameAs(OwnerDataSummary other) =>
      sourceVersion == other.sourceVersion &&
      sample == other.sample &&
      dataSha256 == other.dataSha256 &&
      pointCount == other.pointCount &&
      firstObservedAt == other.firstObservedAt &&
      lastObservedAt == other.lastObservedAt &&
      fetchedAt == other.fetchedAt &&
      switch ((this, other)) {
        (OwnerSecurityPriceSummary a, OwnerSecurityPriceSummary b) =>
          a.instrumentId == b.instrumentId &&
              a.symbol == b.symbol &&
              a.venue == b.venue &&
              a.currency == b.currency &&
              a.priceAdjustment == b.priceAdjustment,
        (OwnerFxSummary a, OwnerFxSummary b) =>
          a.baseCurrency == b.baseCurrency &&
              a.quoteCurrency == b.quoteCurrency,
        (OwnerBenchmarkSummary a, OwnerBenchmarkSummary b) =>
          a.benchmarkId == b.benchmarkId &&
              a.label == b.label &&
              a.currency == b.currency &&
              a.returnBasis == b.returnBasis &&
              a.sourceReference == b.sourceReference,
        _ => false,
      };
}

final class OwnerSecurityPriceSummary extends OwnerDataSummary {
  const OwnerSecurityPriceSummary({
    required this.instrumentId,
    required this.symbol,
    required this.venue,
    required this.currency,
    required this.priceAdjustment,
    required super.sourceVersion,
    required super.sample,
    required super.dataSha256,
    required super.pointCount,
    required super.firstObservedAt,
    required super.lastObservedAt,
    required super.fetchedAt,
  });

  final String instrumentId, symbol, venue, currency, priceAdjustment;
  @override
  String get kind => 'security_price';
}

final class OwnerFxSummary extends OwnerDataSummary {
  const OwnerFxSummary({
    required this.baseCurrency,
    required this.quoteCurrency,
    required super.sourceVersion,
    required super.sample,
    required super.dataSha256,
    required super.pointCount,
    required super.firstObservedAt,
    required super.lastObservedAt,
    required super.fetchedAt,
  });

  final String baseCurrency, quoteCurrency;
  @override
  String get kind => 'fx';
}

final class OwnerBenchmarkSummary extends OwnerDataSummary {
  const OwnerBenchmarkSummary({
    required this.benchmarkId,
    required this.label,
    required this.currency,
    required this.returnBasis,
    required this.sourceReference,
    required super.sourceVersion,
    required super.sample,
    required super.dataSha256,
    required super.pointCount,
    required super.firstObservedAt,
    required super.lastObservedAt,
    required super.fetchedAt,
  });

  final String benchmarkId, label, currency, returnBasis, sourceReference;
  @override
  String get kind => 'benchmark';
}
