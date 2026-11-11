import 'dart:async';
import 'package:http/http.dart' as http;
import 'package:omni_folio_client/core/data/http_transport.dart';
import 'package:omni_folio_client/core/domain/api_exception.dart';
import 'package:omni_folio_client/features/research/data/public_research_codec.dart';
import 'package:omni_folio_client/features/research/data/research_report_codec.dart';
import 'package:omni_folio_client/features/research/domain/public_research.dart';
import 'package:omni_folio_client/features/research/domain/research_report.dart';
import 'package:omni_folio_client/features/research/domain/research_repository.dart';

class RestResearchRepository implements ResearchRepository {
  RestResearchRepository(this.transport);
  final HttpTransport transport;

  @override
  Future<ResearchReport> researchReport() async =>
      decodeResearchReport(await transport.get('/v1/research/report'));

  @override
  Future<PublicResearch> publicResearch(PublicResearchIdentity identity) async {
    final abort = Completer<void>();
    try {
      final response = await transport.client
          .send(
            http.AbortableRequest(
              'GET',
              transport
                  .uri('/v1/research/feed')
                  .replace(
                    queryParameters: {
                      'market': identity.market,
                      'symbol': identity.symbol,
                    },
                  ),
              abortTrigger: abort.future,
            ),
          )
          .timeout(const Duration(seconds: 15));
      final json = await transport
          .boundedObject(response, 6 * 1024 * 1024 + 4096)
          .timeout(const Duration(seconds: 15));
      return decodePublicResearch(json, identity);
    } catch (error) {
      if (error is ApiException || error is FormatException) rethrow;
      throw const ApiException(apiConnectionError);
    } finally {
      if (!abort.isCompleted) abort.complete();
    }
  }
}
