import 'dart:async';
import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:omni_folio_client/core/data/http_transport.dart';
import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/core/domain/api_exception.dart';
import 'package:omni_folio_client/features/watchlist/data/codecs.dart';
import 'package:omni_folio_client/features/watchlist/domain/models.dart';
import 'package:omni_folio_client/features/watchlist/domain/watchlist_repository.dart';

class RestWatchlistRepository implements WatchlistRepository {
  RestWatchlistRepository(this.transport);
  final HttpTransport transport;

  Future<Json> _watchlistRequest([WatchlistChange? change]) async {
    final abort = Completer<void>();
    Future<Json> request() async {
      final req = http.AbortableRequest(
        change == null ? 'GET' : 'POST',
        transport.uri('/v1/watchlist'),
        abortTrigger: abort.future,
      );
      if (change != null) {
        final bytes = utf8.encode(jsonEncode(encodeWatchlistChange(change)));
        if (bytes.length > 16 * 1024) {
          throw const FormatException('Watchlist request too large');
        }
        req.headers['content-type'] = 'application/json';
        req.bodyBytes = bytes;
      }
      return transport.boundedObject(
        await transport.client.send(req),
        128 * 1024,
      );
    }

    try {
      return await request().timeout(
        const Duration(seconds: 40),
        onTimeout: () {
          abort.complete();
          throw const ApiException(apiConnectionError);
        },
      );
    } catch (error) {
      if (error is ApiException || error is FormatException) rethrow;
      throw const ApiException(apiConnectionError);
    }
  }

  @override
  Future<WatchlistReport> watchlist() async =>
      decodeWatchlistReport(await _watchlistRequest());

  @override
  Future<WatchlistState> updateWatchlist(WatchlistChange change) async {
    final state = decodeWatchlistState(await _watchlistRequest(change));
    if (!change.matchesAck(state)) {
      throw const FormatException('Watchlist acknowledgement mismatch');
    }
    return state;
  }
}
