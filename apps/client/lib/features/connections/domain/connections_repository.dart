import 'package:omni_folio_client/features/connections/domain/models.dart';

abstract interface class ConnectionsRepository {
  Future<ServiceStatus> status();
  Future<BrokerReconciliation?> latestBrokerReconciliation();
  Future<LocalOrderLog> localOrders();
}
