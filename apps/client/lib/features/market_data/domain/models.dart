import 'package:omni_folio_client/features/market_data/domain/owner_data_summary.dart';

class OwnerDataPreview {
  const OwnerDataPreview(this.input, this.summary);
  final String input;
  final OwnerDataSummary summary;
  String get kind => summary.kind;
}

class OwnerDataReceipt {
  const OwnerDataReceipt(this.review, this.recordedAt);

  final OwnerDataPreview review;
  final String recordedAt;
}

class MarketIssue {
  const MarketIssue({required this.code, required this.message});

  final String code;
  final String message;
}

class MarketBar {
  const MarketBar({
    required this.at,
    required this.open,
    required this.high,
    required this.low,
    required this.close,
    required this.volume,
  });

  final String at;
  final String open;
  final String high;
  final String low;
  final String close;
  final String volume;
}

class MarketCandles {
  const MarketCandles({
    required this.instrumentId,
    required this.currency,
    required this.barTimeBasis,
    required this.symbol,
    required this.venue,
    required this.timezone,
    required this.interval,
    required this.priceAdjustment,
    required this.source,
    required this.sample,
    required this.state,
    required this.sourceAsOf,
    required this.fetchedAt,
    required this.issues,
    required this.bars,
  });

  final String? instrumentId;
  final String? currency;
  final String barTimeBasis;
  final String symbol;
  final String venue;
  final String timezone;
  final String interval;
  final String priceAdjustment;
  final String source;
  final bool sample;
  final String state;
  final String? sourceAsOf;
  final String fetchedAt;
  final List<MarketIssue> issues;
  final List<MarketBar> bars;
}
