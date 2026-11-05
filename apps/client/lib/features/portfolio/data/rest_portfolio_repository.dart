import 'dart:async';
import 'package:http/http.dart' as http;
import 'package:omni_folio_client/core/data/http_transport.dart';
import 'package:omni_folio_client/core/domain/api_exception.dart';
import 'package:omni_folio_client/features/portfolio/data/codecs.dart';
import 'package:omni_folio_client/features/portfolio/domain/models.dart';
import 'package:omni_folio_client/features/portfolio/domain/portfolio_repository.dart';

class RestPortfolioRepository implements PortfolioRepository {
  RestPortfolioRepository(this.transport);
  final HttpTransport transport;

  @override
  Future<PortfolioSnapshot> snapshot() async =>
      decodePortfolioSnapshot(await transport.get('/v1/portfolio/snapshot'));

  @override
  Future<CurrentPortfolioValuation> currentPortfolioValuation() async =>
      decodeCurrentPortfolioValuation(
        await transport.get('/v1/portfolio/current-valuation'),
      );

  @override
  Future<PortfolioXirr> portfolioXirr() async =>
      decodePortfolioXirr(await transport.get('/v1/portfolio/xirr'));

  @override
  Future<PortfolioTwr> portfolioTwr() async =>
      decodePortfolioTwr(await transport.get('/v1/portfolio/twr'));

  @override
  Future<PortfolioTwrCheckpoints> portfolioTwrCheckpoints() async {
    final abort = Completer<void>();
    Future<PortfolioTwrCheckpoints> request() async {
      final response = await transport.client.send(
        http.AbortableRequest(
          'GET',
          transport.uri('/v1/portfolio/twr/checkpoints'),
          abortTrigger: abort.future,
        ),
      );
      return decodePortfolioTwrCheckpoints(
        await transport.boundedObject(response, 3 * (1 << 20)),
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
  Future<PortfolioBenchmarks> portfolioBenchmarks() async =>
      decodePortfolioBenchmarks(
        await transport.get('/v1/portfolio/benchmarks'),
      );

  @override
  Future<HoldingValuation> holdingValuation() async => decodeHoldingValuation(
    await transport.get('/v1/portfolio/holding-valuation'),
  );
}
