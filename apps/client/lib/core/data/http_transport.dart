import 'dart:typed_data';
import 'package:http/http.dart' as http;
import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/core/domain/api_exception.dart';
import 'package:omni_folio_client/features/portfolio/data/codecs.dart';

const defaultApiUrl = String.fromEnvironment(
  'OMNI_API_URL',
  defaultValue: 'http://127.0.0.1:8080',
);
const apiConnectionError = 'API에 연결할 수 없습니다. 서버 주소를 확인한 뒤 다시 시도하세요.';

class HttpTransport {
  HttpTransport({http.Client? client, String? baseUrl})
    : client = client ?? http.Client(),
      baseUrl = (baseUrl ?? defaultApiUrl).replaceFirst(RegExp(r'/$'), '');
  final http.Client client;
  final String baseUrl;
  Uri uri(String path) => Uri.parse('$baseUrl$path');

  Future<Json> boundedObject(http.StreamedResponse response, int limit) async {
    final body = BytesBuilder(copy: false);
    await for (final chunk in response.stream) {
      if (body.length + chunk.length > limit) {
        throw const FormatException('Response exceeds its declared bound');
      }
      body.add(chunk);
    }
    return object(
      http.Response.bytes(
        body.takeBytes(),
        response.statusCode,
        headers: {'content-type': 'application/json; charset=utf-8'},
      ),
    );
  }

  Future<Json> get(String path) => getUri(uri(path));

  Future<Json> getUri(Uri uri) async {
    try {
      final response = await client.get(uri);
      return object(response);
    } catch (error) {
      if (error is ApiException || error is FormatException) rethrow;
      throw const ApiException(apiConnectionError);
    }
  }

  Future<Json> post(
    String path, {
    required String contentType,
    required Object body,
  }) async {
    try {
      final response = await client.post(
        uri(path),
        headers: {'content-type': contentType},
        body: body,
      );
      return object(response);
    } catch (error) {
      if (error is ApiException || error is FormatException) rethrow;
      throw const ApiException(apiConnectionError);
    }
  }

  Json object(http.Response response) {
    if (response.statusCode >= 200 && response.statusCode < 300) {
      return decodeObject(response.body);
    }
    try {
      final error = decodeObject(response.body);
      throw ApiException(
        error['message'] is String
            ? error['message'] as String
            : '요청을 완료하지 못했습니다.',
        statusCode: response.statusCode,
        code: error['code'] is String ? error['code'] as String : null,
      );
    } on FormatException {
      throw ApiException('요청을 완료하지 못했습니다 (${response.statusCode}).');
    }
  }
}
