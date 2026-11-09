import 'package:omni_folio_client/core/domain/timestamp.dart';
import 'package:omni_folio_client/core/domain/models.dart';

class Holding {
  const Holding({
    required this.instrumentId,
    required this.symbol,
    required this.quantity,
    required this.costBasis,
    required this.currency,
  });

  final String instrumentId;
  final String symbol;
  final String quantity;
  final String costBasis;
  final String currency;
}

class HoldingValuationIssue {
  const HoldingValuationIssue({
    required this.code,
    required this.message,
    required this.field,
  });

  final String code;
  final String message;
  final String? field;
}

class HoldingValuationPrice {
  const HoldingValuationPrice({
    required this.source,
    this.sourceVersion,
    required this.venue,
    required this.currency,
    required this.price,
    required this.priceAdjustment,
    required this.observedAt,
    required this.fetchedAt,
    required this.recordedAt,
    required this.sample,
    required this.state,
  });

  final String source;
  final String? sourceVersion;
  final String venue;
  final String currency;
  final String price;
  final String priceAdjustment;
  final String observedAt;
  final String fetchedAt;
  final String recordedAt;
  final bool sample;
  final String state;
}

class HoldingValuationLine {
  const HoldingValuationLine({
    required this.symbol,
    required this.quantity,
    required this.costBasis,
    required this.currency,
    required this.status,
    required this.price,
    required this.marketValue,
    required this.unrealizedPnl,
    required this.issue,
  });

  final String symbol;
  final String quantity;
  final String costBasis;
  final String currency;
  final String status;
  final HoldingValuationPrice? price;
  final String? marketValue;
  final String? unrealizedPnl;
  final HoldingValuationIssue? issue;
}

class HoldingValuationTotal {
  const HoldingValuationTotal({
    required this.currency,
    required this.costBasis,
    required this.marketValue,
    required this.unrealizedPnl,
  });

  final String currency;
  final String costBasis;
  final String marketValue;
  final String unrealizedPnl;
}

class HoldingValuation {
  const HoldingValuation({
    required this.valuationAsOf,
    required this.ledgerRevision,
    required this.ledgerAsOf,
    required this.ledgerRecordedAt,
    required this.status,
    required this.sample,
    required this.totals,
    required this.lines,
    required this.issues,
    this.maxObservationAgeSeconds = 86400,
  });

  final int maxObservationAgeSeconds;
  final String valuationAsOf;
  final String ledgerRevision;
  final String ledgerAsOf;
  final String ledgerRecordedAt;
  final String status;
  final bool sample;
  final List<HoldingValuationTotal> totals;
  final List<HoldingValuationLine> lines;
  final List<HoldingValuationIssue> issues;
  bool get ownerDeclaredPrice =>
      issues.any((i) => i.code == 'owner_declared_price');
}

class CurrentPortfolioValuation {
  const CurrentPortfolioValuation({
    required this.baseCurrency,
    required this.valuationAsOf,
    required this.ledgerRevision,
    required this.ledgerAsOf,
    required this.ledgerRecordedAt,
    required this.status,
    required this.sample,
    required this.cashValue,
    required this.holdingsValue,
    required this.totalValue,
    required this.issues,
    this.maxObservationAgeSeconds = 86400,
  });

  final int maxObservationAgeSeconds;
  final String baseCurrency;
  final String valuationAsOf;
  final String ledgerRevision;
  final String ledgerAsOf;
  final String ledgerRecordedAt;
  final String status;
  final bool sample;
  final Money? cashValue;
  final Money? holdingsValue;
  final Money? totalValue;
  final List<HoldingValuationIssue> issues;
  bool get ownerDeclaredFx => issues.any((i) => i.code == 'owner_declared_fx');
  bool get ownerDeclaredPrice =>
      issues.any((i) => i.code == 'owner_declared_price');
  int get missingInputCount => issues
      .where(
        (i) =>
            i.code != 'owner_declared_fx' && i.code != 'owner_declared_price',
      )
      .length;
}

class PortfolioSnapshot {
  const PortfolioSnapshot({
    required this.ledgerRevision,
    required this.costBasisPolicy,
    required this.recordedAt,
    required this.cash,
    required this.holdings,
    required this.realizedPnl,
    this.eventCount = 0,
    this.receiptCount = 0,
  });

  final String ledgerRevision;
  final String costBasisPolicy;
  final DateTime recordedAt;
  final List<Money> cash;
  final List<Holding> holdings;
  final List<Money> realizedPnl;
  final int eventCount;
  final int receiptCount;
}

class TwrCheckpoint {
  const TwrCheckpoint(this.at, this.kind, this.cumulativeRate);
  final String at, kind, cumulativeRate;
  BigInt get atNanoseconds => utcNanoseconds(at);
}

class PortfolioTwrCheckpoints {
  PortfolioTwrCheckpoints(this.report, List<TwrCheckpoint> points)
    : points = List.unmodifiable(points);

  final PortfolioTwr report;
  final List<TwrCheckpoint> points;
}

class PortfolioTwr {
  const PortfolioTwr({
    required this.valuation,
    required this.status,
    required this.sample,
    required this.periodStart,
    required this.periodEnd,
    required this.externalEventCount,
    required this.boundaryCount,
    required this.cumulativeRate,
    required this.netContributions,
    required this.issues,
  });

  final CurrentPortfolioValuation valuation;
  final String status;
  final bool sample;
  final String? periodStart;
  final String periodEnd;
  final int? externalEventCount;
  final int? boundaryCount;
  final String? cumulativeRate;
  final Money? netContributions;
  final List<HoldingValuationIssue> issues;
  bool get ownerDeclaredFx => issues.any((i) => i.code == 'owner_declared_fx');
  bool get ownerDeclaredPrice =>
      issues.any((i) => i.code == 'owner_declared_price');
}

class BenchmarkSeriesSummary {
  const BenchmarkSeriesSummary({
    required this.id,
    required this.label,
    required this.currency,
    required this.returnBasis,
    required this.sourceReference,
    required this.sourceVersion,
    required this.sample,
    required this.pointCount,
    required this.firstObservedAt,
    required this.lastObservedAt,
    required this.fetchedAt,
    required this.recordedAt,
  });

  final String id, label, currency, returnBasis, sourceReference, sourceVersion;
  final bool sample;
  final int pointCount;
  final String firstObservedAt, lastObservedAt, fetchedAt, recordedAt;
}

class BenchmarkRate {
  const BenchmarkRate(this.status, this.rate, this.issue);

  final String status;
  final String? rate, issue;
}

class PortfolioBenchmark {
  const PortfolioBenchmark({
    required this.series,
    required this.sample,
    required this.start,
    required this.end,
    required this.startFx,
    required this.endFx,
    required this.native,
    required this.krw,
    required this.differencePercentagePoints,
  });

  final BenchmarkSeriesSummary series;
  final bool sample;
  final BenchmarkObservation? start, end;
  final BenchmarkFxObservation? startFx, endFx;
  final BenchmarkRate native, krw;
  final String? differencePercentagePoints;
  bool get ownerDeclaredFx =>
      startFx?.sourceVersion != null || endFx?.sourceVersion != null;
}

class PortfolioBenchmarks {
  const PortfolioBenchmarks(this.portfolio, this.benchmarks, this.issues);

  final PortfolioTwr portfolio;
  final List<PortfolioBenchmark> benchmarks;
  final List<HoldingValuationIssue> issues;
  bool get ownerDeclaredPrice => portfolio.ownerDeclaredPrice;
  bool get sample => portfolio.sample || benchmarks.any((row) => row.sample);
  bool get ownerDeclaredFx =>
      portfolio.ownerDeclaredFx || benchmarks.any((row) => row.ownerDeclaredFx);
}

class PortfolioXirr {
  const PortfolioXirr({
    required this.valuation,
    required this.status,
    required this.sample,
    required this.periodStart,
    required this.periodEnd,
    required this.externalEventCount,
    required this.signChanges,
    required this.annualRate,
    required this.candidateRate,
    required this.netContributions,
    required this.issues,
  });

  final CurrentPortfolioValuation valuation;
  final String status;
  final bool sample;
  final String? periodStart;
  final String periodEnd;
  final int? externalEventCount;
  final int? signChanges;
  final String? annualRate;
  final String? candidateRate;
  final Money? netContributions;
  final List<HoldingValuationIssue> issues;
  bool get ownerDeclaredFx => issues.any((i) => i.code == 'owner_declared_fx');
  bool get ownerDeclaredPrice =>
      issues.any((i) => i.code == 'owner_declared_price');
}

typedef BenchmarkObservation = ({String observedAt, String value});

typedef BenchmarkFxObservation = ({
  String observedAt,
  String value,
  String? sourceVersion,
  bool sample,
});
