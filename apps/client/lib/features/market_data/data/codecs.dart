import 'package:omni_folio_client/core/domain/timestamp.dart';
import 'dart:convert';
import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/core/domain/decimal.dart';
import 'package:omni_folio_client/features/market_data/domain/models.dart';
import 'package:omni_folio_client/features/market_data/domain/owner_data_summary.dart';

OwnerDataPreview decodeOwnerDataPreview(Json json, String input) {
  jsonRequireExactKeys(json, {'kind', 'input', 'summary'});
  final kind = jsonText(json, 'kind');
  if (json['input'] != input || json['summary'] is! Json) {
    throw const FormatException('Owner input review does not match request');
  }
  final summary = json['summary'] as Json;
  final source = decodeObject(input);
  final schema = switch (kind) {
    'security_price' => 'owner-price-series.v1',
    'fx' => 'owner-fx-series.v1',
    'benchmark' => 'benchmark-index-series.v1',
    _ => throw const FormatException('Unknown owner data kind'),
  };
  final identityKeys = switch (kind) {
    'security_price' => {
      'instrument_id',
      'symbol',
      'venue',
      'currency',
      'price_adjustment',
    },
    'fx' => {'base_currency', 'quote_currency'},
    _ => {
      'benchmark_id',
      'label',
      'currency',
      'return_basis',
      'source_reference',
    },
  };
  jsonRequireExactKeys(summary, {
    ...identityKeys,
    'authority',
    'source_version',
    'sample',
    'data_sha256',
    'point_count',
    'first_observed_at',
    'last_observed_at',
    'fetched_at',
  });
  if (source['schema_version'] != schema ||
      summary['authority'] != 'owner_declared') {
    throw const FormatException('Invalid owner data authority');
  }
  for (final key in {
    ...identityKeys,
    'source_version',
    'sample',
    'fetched_at',
  }) {
    if (summary[key] != source[key]) {
      throw const FormatException('Owner metadata does not match input');
    }
  }
  jsonBenchmarkText(summary, 'source_version', 128, identity: true);
  jsonBool(summary, 'sample');
  for (final key in identityKeys) {
    if (key.contains('currency')) {
      jsonCurrency(summary, key);
    } else {
      jsonBenchmarkText(
        summary,
        key,
        key == 'source_reference'
            ? 512
            : key == 'label'
            ? 160
            : 128,
        identity: key != 'label',
        trimmed: key == 'label',
      );
    }
  }
  if (kind == 'security_price' &&
      (summary['price_adjustment'] != 'unadjusted' ||
          summary['symbol'] != (summary['symbol'] as String).toUpperCase() ||
          summary['venue'] != (summary['venue'] as String).toUpperCase())) {
    throw const FormatException('Invalid owner price identity');
  }
  if (kind == 'fx' && summary['base_currency'] == summary['quote_currency']) {
    throw const FormatException('Invalid FX direction');
  }
  if (kind == 'benchmark') {
    final uri = Uri.tryParse(summary['source_reference'] as String);
    if (!const {'KRW', 'USD'}.contains(summary['currency']) ||
        !const {
          'price_index',
          'gross_total_return',
          'net_total_return',
        }.contains(summary['return_basis']) ||
        uri == null ||
        uri.scheme != 'https' ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment) {
      throw const FormatException('Invalid benchmark metadata');
    }
  }
  final fetched = jsonUtcText(summary, 'fetched_at');
  final count = jsonCount(summary, 'point_count');
  final points = source['points'];
  if (points is! List ||
      points.length != count ||
      count > 10000 ||
      (kind == 'benchmark' && count == 0) ||
      !RegExp(r'^[0-9a-f]{64}$').hasMatch(jsonText(summary, 'data_sha256'))) {
    throw const FormatException('Invalid owner data point summary');
  }
  if (count == 0) {
    if (summary['first_observed_at'] != null ||
        summary['last_observed_at'] != null) {
      throw const FormatException('Empty owner series has observations');
    }
  } else {
    final first = jsonUtcText(summary, 'first_observed_at');
    final last = jsonUtcText(summary, 'last_observed_at');
    if (points.first is! Json ||
        points.last is! Json ||
        points.first['observed_at'] != first ||
        points.last['observed_at'] != last ||
        utcNanoseconds(first) > utcNanoseconds(last) ||
        ((count == 1) != (first == last)) ||
        utcNanoseconds(last) > utcNanoseconds(fetched)) {
      throw const FormatException(
        'Owner observation range does not match input',
      );
    }
  }
  return OwnerDataPreview(input, decodeOwnerDataSummary(kind, summary));
}

OwnerDataReceipt decodeOwnerDataReceipt(Json json, OwnerDataPreview review) {
  jsonRequireExactKeys(json, {'kind', 'receipt'});
  if (json['kind'] != review.kind || json['receipt'] is! Json) {
    throw const FormatException('Owner receipt kind does not match review');
  }
  final receipt = json['receipt'] as Json;
  final expected = encodeOwnerDataSummary(review.summary);
  jsonRequireExactKeys(receipt, {
    ...expected.keys,
    'storage_check',
    'recorded_at',
  });
  if (receipt['storage_check'] != 'verified' ||
      expected.keys.any((key) => receipt[key] != expected[key])) {
    throw const FormatException('Owner receipt does not match review');
  }
  final recorded = jsonUtcText(receipt, 'recorded_at');
  if (utcNanoseconds(recorded) < utcNanoseconds(review.summary.fetchedAt)) {
    throw const FormatException('Owner receipt predates capture');
  }
  return OwnerDataReceipt(review, recorded);
}

MarketIssue decodeMarketIssue(Json json) => MarketIssue(
  code: jsonText(json, 'code'),
  message: jsonText(json, 'message'),
);

MarketBar decodeMarketBar(Json json) {
  final open = _positiveDecimal(json, 'open');
  final high = _positiveDecimal(json, 'high');
  final low = _positiveDecimal(json, 'low');
  final close = _positiveDecimal(json, 'close');
  final volume = _nonnegativeDecimal(json, 'volume');
  if (comparePositiveDecimal(low, open) > 0 ||
      comparePositiveDecimal(low, close) > 0 ||
      comparePositiveDecimal(high, open) < 0 ||
      comparePositiveDecimal(high, close) < 0) {
    throw const FormatException('OHLC values are inconsistent');
  }
  final at = jsonRfc3339Text(json['at']);
  return MarketBar(
    at: at,
    open: open,
    high: high,
    low: low,
    close: close,
    volume: volume,
  );
}

String _positiveDecimal(Json json, String key) {
  final value = jsonDecimal(json, key);
  if (value == '0' || value.startsWith('-')) {
    throw const FormatException('Price must be positive');
  }
  return _drawableDecimal(value);
}

String _nonnegativeDecimal(Json json, String key) {
  final value = jsonDecimal(json, key);
  if (value.startsWith('-')) {
    throw const FormatException('Volume must be nonnegative');
  }
  return _drawableDecimal(value);
}

String _drawableDecimal(String value) {
  final parsed = double.tryParse(value);
  if (parsed == null || !parsed.isFinite) {
    throw const FormatException('Decimal is outside the drawable range');
  }
  return value;
}

MarketCandles decodeMarketCandles(Json json) {
  final instrumentId = json['instrument_id'];
  final currency = json['currency'];
  final barTimeBasis = json['bar_time_basis'] ?? 'unspecified';
  if ((instrumentId == null) != (currency == null) ||
      (instrumentId != null &&
          (instrumentId is! String ||
              instrumentId.isEmpty ||
              utf8.encode(instrumentId).length > 128 ||
              RegExp(
                r'\s|[\u0000-\u001f\u007f-\u009f]',
              ).hasMatch(instrumentId) ||
              currency is! String ||
              !RegExp(r'^[A-Z]{3}$').hasMatch(currency))) ||
      barTimeBasis != 'unspecified') {
    throw const FormatException('Invalid market source identity or time basis');
  }
  final source = jsonText(json, 'source');
  final state = jsonText(json, 'state');
  final priceAdjustment = jsonText(json, 'price_adjustment');
  if (!const {'empty', 'partial', 'stale', 'success'}.contains(state)) {
    throw const FormatException('Unsupported market state');
  }
  if (!const {'unspecified', 'provider_adjusted'}.contains(priceAdjustment)) {
    throw const FormatException('Unsupported market price adjustment');
  }
  if (jsonText(json, 'interval') != '1d') {
    throw const FormatException('Unsupported market interval');
  }
  final sample = jsonBool(json, 'sample');
  final sourceAsOf = jsonOptionalRfc3339Text(json, 'source_as_of');
  final barJson = jsonJsonList(json, 'bars');
  if (barJson.length > 500) {
    throw const FormatException('Too many market bars');
  }
  final bars = barJson.map(decodeMarketBar).toList(growable: false);
  for (var index = 1; index < bars.length; index++) {
    if (!jsonRfc3339(
      bars[index - 1].at,
    ).isBefore(jsonRfc3339(bars[index].at))) {
      throw const FormatException('Bars must be strictly ordered');
    }
  }
  final issues = jsonJsonList(
    json,
    'issues',
  ).map(decodeMarketIssue).toList(growable: false);
  final valid = switch (state) {
    'empty' => bars.isEmpty && issues.isEmpty,
    'success' => bars.isNotEmpty && issues.isEmpty,
    'partial' => bars.isNotEmpty && issues.isNotEmpty,
    'stale' => bars.isNotEmpty,
    _ => false,
  };
  if (!valid) throw const FormatException('Inconsistent market state');
  if (source == 'local_fixture' &&
      (!sample ||
          priceAdjustment != 'unspecified' ||
          state != 'stale' ||
          sourceAsOf == null ||
          issues.isEmpty)) {
    throw const FormatException('Inconsistent local fixture provenance');
  }
  return MarketCandles(
    instrumentId: instrumentId as String?,
    currency: currency as String?,
    barTimeBasis: barTimeBasis as String,
    symbol: jsonText(json, 'symbol'),
    venue: jsonText(json, 'venue'),
    timezone: jsonText(json, 'timezone'),
    interval: '1d',
    priceAdjustment: priceAdjustment,
    source: source,
    sample: sample,
    state: state,
    sourceAsOf: sourceAsOf,
    fetchedAt: jsonRfc3339Text(json['fetched_at']),
    issues: issues,
    bars: bars,
  );
}

OwnerDataSummary decodeOwnerDataSummary(String kind, Json summary) =>
    switch (kind) {
      'security_price' => OwnerSecurityPriceSummary(
        instrumentId: summary['instrument_id'] as String,
        symbol: summary['symbol'] as String,
        venue: summary['venue'] as String,
        currency: summary['currency'] as String,
        priceAdjustment: summary['price_adjustment'] as String,
        sourceVersion: summary['source_version'] as String,
        sample: summary['sample'] as bool,
        dataSha256: summary['data_sha256'] as String,
        pointCount: summary['point_count'] as int,
        firstObservedAt: summary['first_observed_at'] as String?,
        lastObservedAt: summary['last_observed_at'] as String?,
        fetchedAt: summary['fetched_at'] as String,
      ),
      'fx' => OwnerFxSummary(
        baseCurrency: summary['base_currency'] as String,
        quoteCurrency: summary['quote_currency'] as String,
        sourceVersion: summary['source_version'] as String,
        sample: summary['sample'] as bool,
        dataSha256: summary['data_sha256'] as String,
        pointCount: summary['point_count'] as int,
        firstObservedAt: summary['first_observed_at'] as String?,
        lastObservedAt: summary['last_observed_at'] as String?,
        fetchedAt: summary['fetched_at'] as String,
      ),
      'benchmark' => OwnerBenchmarkSummary(
        benchmarkId: summary['benchmark_id'] as String,
        label: summary['label'] as String,
        currency: summary['currency'] as String,
        returnBasis: summary['return_basis'] as String,
        sourceReference: summary['source_reference'] as String,
        sourceVersion: summary['source_version'] as String,
        sample: summary['sample'] as bool,
        dataSha256: summary['data_sha256'] as String,
        pointCount: summary['point_count'] as int,
        firstObservedAt: summary['first_observed_at'] as String?,
        lastObservedAt: summary['last_observed_at'] as String?,
        fetchedAt: summary['fetched_at'] as String,
      ),
      _ => throw const FormatException('Unknown owner data kind'),
    };

Json encodeOwnerDataSummary(OwnerDataSummary summary) => {
  'authority': 'owner_declared',
  'source_version': summary.sourceVersion,
  'sample': summary.sample,
  'data_sha256': summary.dataSha256,
  'point_count': summary.pointCount,
  'first_observed_at': summary.firstObservedAt,
  'last_observed_at': summary.lastObservedAt,
  'fetched_at': summary.fetchedAt,
  ...switch (summary) {
    OwnerSecurityPriceSummary value => {
      'instrument_id': value.instrumentId,
      'symbol': value.symbol,
      'venue': value.venue,
      'currency': value.currency,
      'price_adjustment': value.priceAdjustment,
    },
    OwnerFxSummary value => {
      'base_currency': value.baseCurrency,
      'quote_currency': value.quoteCurrency,
    },
    OwnerBenchmarkSummary value => {
      'benchmark_id': value.benchmarkId,
      'label': value.label,
      'currency': value.currency,
      'return_basis': value.returnBasis,
      'source_reference': value.sourceReference,
    },
  },
};
