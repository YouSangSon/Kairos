import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/features/watchlist/domain/models.dart';

WatchlistEntry decodeWatchlistEntry(Json json) {
  jsonRequireExactKeys(json, {'venue', 'symbol', 'currency'});
  final venue = jsonText(json, 'venue');
  final symbol = jsonText(json, 'symbol');
  final currency = jsonText(json, 'currency');
  final kr =
      venue == 'XKRX' &&
      currency == 'KRW' &&
      RegExp(r'^[0-9]{6}$').stringMatch(symbol) == symbol;
  final us =
      const {'XNAS', 'XNYS'}.contains(venue) &&
      currency == 'USD' &&
      RegExp(r'^[A-Z0-9][A-Z0-9.-]{0,31}$').stringMatch(symbol) == symbol;
  if (!kr && !us) throw const FormatException('Invalid watchlist identity');
  return WatchlistEntry(venue, symbol, currency);
}

WatchlistState decodeWatchlistState(Json json) {
  jsonRequireExactKeys(json, {'revision', 'updated_at', 'entries'});
  final revision = jsonCount(json, 'revision');
  final updated = json['updated_at'] == null
      ? null
      : jsonUtcText(json, 'updated_at');
  final raw = jsonJsonList(json, 'entries');
  if (revision > 9007199254740991 ||
      raw.length > 50 ||
      (revision == 0 ? updated != null || raw.isNotEmpty : updated == null)) {
    throw const FormatException('Invalid watchlist state');
  }
  final entries = raw.map(decodeWatchlistEntry).toList();
  for (var i = 1; i < entries.length; i++) {
    if (entries[i - 1].key.compareTo(entries[i].key) >= 0) {
      throw const FormatException('Watchlist must be unique and sorted');
    }
  }
  return WatchlistState(revision, updated, List.unmodifiable(entries));
}

WatchlistPrice decodeWatchlistPrice(Json json, String cutoff, String status) {
  jsonRequireExactKeys(json, {
    'source',
    'source_version',
    'price',
    'price_adjustment',
    'observed_at',
    'fetched_at',
    'recorded_at',
    'sample',
  });
  final source = jsonText(json, 'source');
  final version = json['source_version'] == null
      ? null
      : jsonBenchmarkText(json, 'source_version', 128, identity: true);
  final price = jsonDecimal(json, 'price');
  final adjustment = jsonText(json, 'price_adjustment');
  final observed = jsonUtcText(json, 'observed_at'),
      fetched = jsonUtcText(json, 'fetched_at'),
      recorded = jsonUtcText(json, 'recorded_at');
  final sample = jsonBool(json, 'sample');
  final owner =
      source == 'owner_declared' &&
      version != null &&
      adjustment == 'unadjusted';
  final fixture =
      source == 'local_fixture' &&
      version == null &&
      adjustment == 'unspecified' &&
      sample;
  final old =
      jsonUtcNanoseconds(cutoff) - jsonUtcNanoseconds(observed) >
      BigInt.from(86400) * BigInt.from(1000000000);
  if ((!owner && !fixture) ||
      price.length > 128 ||
      price.startsWith('-') ||
      price == '0' ||
      jsonUtcNanoseconds(observed) > jsonUtcNanoseconds(fetched) ||
      jsonUtcNanoseconds(fetched) > jsonUtcNanoseconds(recorded) ||
      jsonUtcNanoseconds(recorded) > jsonUtcNanoseconds(cutoff) ||
      status != (fixture || old ? 'stale' : 'unknown')) {
    throw const FormatException('Invalid stored watchlist price');
  }
  return WatchlistPrice(
    source,
    version,
    price,
    adjustment,
    observed,
    fetched,
    recorded,
    sample,
  );
}

WatchlistQuote decodeWatchlistQuote(
  Json json,
  WatchlistEntry expected,
  String cutoff,
) {
  jsonRequireExactKeys(json, {
    'venue',
    'symbol',
    'currency',
    'status',
    'price',
  });
  final entry = decodeWatchlistEntry({
    'venue': json['venue'],
    'symbol': json['symbol'],
    'currency': json['currency'],
  });
  final status = jsonText(json, 'status');
  if (entry.key != expected.key ||
      !const {'missing', 'ambiguous', 'unknown', 'stale'}.contains(status)) {
    throw const FormatException('Watchlist quote identity or status mismatch');
  }
  final hasPrice = status == 'unknown' || status == 'stale';
  if (hasPrice ? json['price'] is! Json : json['price'] != null) {
    throw const FormatException('Watchlist price availability mismatch');
  }
  return WatchlistQuote(
    entry,
    status,
    hasPrice
        ? decodeWatchlistPrice(json['price'] as Json, cutoff, status)
        : null,
  );
}

WatchlistReport decodeWatchlistReport(Json json) {
  jsonRequireExactKeys(json, {
    'policy_version',
    'max_observation_age_seconds',
    'observed_at',
    'watchlist',
    'quotes',
  });
  if (json['policy_version'] != 'watchlist_stored_prices_24h.v1' ||
      json['max_observation_age_seconds'] is! int ||
      json['max_observation_age_seconds'] != 86400 ||
      json['watchlist'] is! Json) {
    throw const FormatException('Unsupported watchlist policy');
  }
  final at = jsonUtcText(json, 'observed_at');
  final state = decodeWatchlistState(json['watchlist'] as Json);
  final rows = jsonJsonList(json, 'quotes');
  if (rows.length != state.entries.length ||
      (state.updatedAt != null &&
          jsonUtcNanoseconds(state.updatedAt!) > jsonUtcNanoseconds(at))) {
    throw const FormatException('Watchlist observation mismatch');
  }
  final quotes = <WatchlistQuote>[];
  for (var i = 0; i < rows.length; i++) {
    quotes.add(decodeWatchlistQuote(rows[i], state.entries[i], at));
  }
  return WatchlistReport(at, state, List.unmodifiable(quotes));
}

Json encodeWatchlistEntry(WatchlistEntry entry) => {
  'venue': entry.venue,
  'symbol': entry.symbol,
  'currency': entry.currency,
};
Json encodeWatchlistChange(WatchlistChange change) => {
  'expected_revision': change.expectedRevision,
  'entries': change.entries.map(encodeWatchlistEntry).toList(),
};
