import 'dart:async';
import 'package:omni_folio_client/core/data/http_transport.dart';
import 'package:omni_folio_client/core/domain/api_exception.dart';
import 'package:omni_folio_client/features/connections/data/codecs.dart';
import 'package:omni_folio_client/features/connections/domain/connections_repository.dart';
import 'package:omni_folio_client/features/connections/domain/models.dart';
import 'package:omni_folio_client/features/portfolio/data/codecs.dart';

class RestConnectionsRepository implements ConnectionsRepository {
  RestConnectionsRepository(this.transport);
  final HttpTransport transport;

  @override
  Future<ServiceStatus> status() async =>
      decodeServiceStatus(await transport.get('/v1/status'));

  @override
  Future<BrokerReconciliation?> latestBrokerReconciliation() async {
    try {
      final response = await transport.client.get(
        transport.uri('/v1/broker-reconciliation/latest'),
      );
      if (response.statusCode == 404) {
        final error = decodeObject(response.body);
        if (error['code'] == 'broker_reconciliation_not_found') return null;
      }
      return decodeBrokerReconciliation(transport.object(response));
    } catch (error) {
      if (error is ApiException || error is FormatException) rethrow;
      throw const ApiException(apiConnectionError);
    }
  }

  @override
  Future<LocalOrderLog> localOrders() async =>
      decodeLocalOrderLog(await transport.get('/v1/orders'));
}
