import 'package:omni_folio_client/features/watchlist/domain/models.dart';

abstract interface class WatchlistRepository {
  Future<WatchlistReport> watchlist();
  Future<WatchlistState> updateWatchlist(WatchlistChange change);
}
