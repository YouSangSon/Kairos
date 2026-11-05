class ServiceStatus {
  const ServiceStatus({
    required this.liveEnabled,
    required this.mode,
    required this.trustState,
    required this.ledgerRevision,
    required this.lastVerifiedAt,
    required this.issues,
  });

  final bool liveEnabled;
  final String mode;
  final String trustState;
  final String ledgerRevision;
  final DateTime? lastVerifiedAt;
  final List<String> issues;
}

class LocalOrderView {
  const LocalOrderView({
    required this.mode,
    required this.symbol,
    required this.side,
    required this.orderType,
    required this.quantity,
    required this.limitPrice,
    required this.filledQuantity,
    required this.currency,
    required this.status,
    required this.pendingAction,
    required this.lastRecordedAt,
  });

  final String mode;
  final String symbol;
  final String side;
  final String orderType;
  final String quantity;
  final String limitPrice;
  final String filledQuantity;
  final String currency;
  final String status;
  final String pendingAction;
  final String lastRecordedAt;
}

class LocalOrderLog {
  const LocalOrderLog({required this.orders});

  final List<LocalOrderView> orders;
}

class BrokerPositionDifference {
  const BrokerPositionDifference({
    required this.symbol,
    required this.brokerQuantity,
    required this.ledgerQuantity,
    required this.difference,
    required this.match,
  });

  final String symbol;
  final String brokerQuantity;
  final String ledgerQuantity;
  final String difference;
  final bool match;
}

class BrokerReconciliation {
  const BrokerReconciliation({
    required this.provider,
    required this.environment,
    required this.exchange,
    required this.freshness,
    required this.fetchedAt,
    required this.recordedAt,
    required this.ledgerRevision,
    required this.allPositionsMatch,
    required this.positionDifferences,
  });

  final String provider;
  final String environment;
  final String exchange;
  final String freshness;
  final String fetchedAt;
  final String recordedAt;
  final String ledgerRevision;
  final bool allPositionsMatch;
  final List<BrokerPositionDifference> positionDifferences;
}
