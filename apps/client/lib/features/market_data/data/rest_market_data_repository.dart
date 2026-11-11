import 'dart:async';
import 'dart:convert';
import 'package:http/http.dart' as http;
import 'package:omni_folio_client/core/data/http_transport.dart';
import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/core/domain/api_exception.dart';
import 'package:omni_folio_client/features/market_data/data/codecs.dart';
import 'package:omni_folio_client/features/market_data/domain/market_data_repository.dart';
import 'package:omni_folio_client/features/market_data/domain/models.dart';

class RestMarketDataRepository implements MarketDataRepository {
  RestMarketDataRepository(this.transport);
  final HttpTransport transport;

  Future<Json> _ownerDataPost(String action, String input) async {
    final abort = Completer<void>();
    Future<Json> request() async {
      final bytes = utf8.encode(input);
      if (bytes.isEmpty || bytes.length > 1 << 20) {
        throw const FormatException('Owner input must be at most 1MiB');
      }
      final req =
          http.AbortableRequest(
              'POST',
              transport.uri('/v1/market-data/imports/$action'),
              abortTrigger: abort.future,
            )
            ..headers['content-type'] = 'application/json'
            ..bodyBytes = bytes;
      final response = await transport.client.send(req);
      return transport.boundedObject(response, 6 * (1 << 20) + 16 * 1024);
    }

    try {
      // A waiting limit cannot prove cancellation or roll back a committed write.
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

  Uri _marketCandlesUri(String symbol) => transport
      .uri('/v1/market-data/candles')
      .replace(queryParameters: {'symbol': symbol, 'interval': '1d'});

  @override
  Future<OwnerDataPreview> previewOwnerData(String input) async =>
      decodeOwnerDataPreview(await _ownerDataPost('preview', input), input);

  @override
  Future<OwnerDataReceipt> applyOwnerData(OwnerDataPreview review) async =>
      decodeOwnerDataReceipt(
        await _ownerDataPost('apply', review.input),
        review,
      );

  @override
  Future<MarketCandles> candles(String symbol) async {
    if (symbol.isEmpty) throw const ApiException('종목 코드가 필요합니다.');
    try {
      final response = await transport.client.get(_marketCandlesUri(symbol));
      final candles = decodeMarketCandles(transport.object(response));
      if (candles.symbol != symbol || candles.interval != '1d') {
        throw const FormatException('Market response does not match request');
      }
      return candles;
    } catch (error) {
      if (error is ApiException || error is FormatException) rethrow;
      throw const ApiException(apiConnectionError);
    }
  }
}
