class WatchlistEntry {
  const WatchlistEntry(this.venue, this.symbol, this.currency);

  final String venue, symbol, currency;
  String get key => '$venue\u0000$symbol\u0000$currency';
}

class WatchlistState {
  const WatchlistState(this.revision, this.updatedAt, this.entries);

  final int revision;
  final String? updatedAt;
  final List<WatchlistEntry> entries;
  bool sameEntries(List<WatchlistEntry> other) {
    if (entries.length != other.length) return false;
    for (var i = 0; i < entries.length; i++) {
      if (entries[i].key != other[i].key) return false;
    }
    return true;
  }
}

class WatchlistChange {
  WatchlistChange({
    required this.expectedRevision,
    required List<WatchlistEntry> entries,
  }) : entries = List.unmodifiable(
         [...entries]..sort((a, b) => a.key.compareTo(b.key)),
       ) {
    if (expectedRevision < 0 ||
        expectedRevision > 9007199254740991 ||
        entries.length > 50) {
      throw const FormatException('Invalid watchlist change');
    }
    for (var i = 1; i < this.entries.length; i++) {
      if (this.entries[i - 1].key == this.entries[i].key) {
        throw const FormatException('Duplicate watchlist entry');
      }
    }
  }
  final int expectedRevision;
  final List<WatchlistEntry> entries;
  bool matchesAck(WatchlistState state) =>
      (state.revision == expectedRevision ||
          state.revision == expectedRevision + 1) &&
      state.sameEntries(entries);
}

class WatchlistPrice {
  const WatchlistPrice(
    this.source,
    this.sourceVersion,
    this.price,
    this.priceAdjustment,
    this.observedAt,
    this.fetchedAt,
    this.recordedAt,
    this.sample,
  );

  final String source,
      price,
      priceAdjustment,
      observedAt,
      fetchedAt,
      recordedAt;
  final String? sourceVersion;
  final bool sample;
}

class WatchlistQuote {
  const WatchlistQuote(this.entry, this.status, this.price);

  final WatchlistEntry entry;
  final String status;
  final WatchlistPrice? price;
}

class WatchlistReport {
  const WatchlistReport(this.observedAt, this.watchlist, this.quotes);

  final String observedAt;
  final WatchlistState watchlist;
  final List<WatchlistQuote> quotes;
}
