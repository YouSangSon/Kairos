class PaperMonitor {
  const PaperMonitor({
    required this.schemaVersion,
    required this.mode,
    required this.observedAt,
    required this.strategySelected,
    required this.sessionCount,
    required this.pendingPolicyCount,
    required this.runner,
    required this.latestPolicy,
  });

  final String schemaVersion;
  final String mode;
  final String observedAt;
  final bool strategySelected;
  final int sessionCount;
  final int pendingPolicyCount;
  final PaperMonitorRunner runner;
  final PaperMonitorPolicy? latestPolicy;
}

class PaperMonitorRunner {
  const PaperMonitorRunner({
    required this.state,
    required this.heartbeatAt,
    required this.expiresAt,
  });

  final String state;
  final String? heartbeatAt;
  final String? expiresAt;
}

class PaperMonitorPolicy {
  const PaperMonitorPolicy({
    required this.asOf,
    required this.recordedAt,
    required this.decision,
    required this.reasonCode,
    required this.matchesCurrentSelection,
  });

  final String asOf;
  final String recordedAt;
  final String decision;
  final String reasonCode;
  final bool matchesCurrentSelection;
}
