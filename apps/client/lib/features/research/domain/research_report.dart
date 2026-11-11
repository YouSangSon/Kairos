enum ResearchPromotion { paperCandidate, noPromotion }

enum ResearchGate { walkForward, finalHoldout, buyAndHoldBaseline }

class ResearchReport {
  const ResearchReport({required this.observedAt, required this.report});

  final String observedAt;
  final StoredResearchReport? report;
}

class StoredResearchReport {
  const StoredResearchReport({
    required this.recordedAt,
    required this.fastWindow,
    required this.slowWindow,
    required this.quantity,
    required this.candidateCount,
    required this.foldCount,
    required this.finalHoldout,
    required this.promotionTarget,
    required this.failedGates,
    required this.walkForward,
    required this.holdout,
    required this.buyAndHold,
  });

  final String recordedAt;
  final int fastWindow;
  final int slowWindow;
  final String quantity;
  final int candidateCount;
  final int foldCount;
  final ResearchHoldout finalHoldout;
  final ResearchPromotion promotionTarget;
  final List<ResearchGate> failedGates;
  final ResearchMetrics walkForward;
  final ResearchMetrics holdout;
  final ResearchMetrics buyAndHold;
}

class ResearchHoldout {
  const ResearchHoldout({
    required this.firstBarAt,
    required this.lastBarAt,
    required this.barCount,
  });

  final String firstBarAt;
  final String lastBarAt;
  final int barCount;
}

class ResearchMetrics {
  const ResearchMetrics({
    required this.afterCostReturn,
    required this.maxDrawdown,
    required this.roundTrips,
  });

  final String afterCostReturn;
  final String maxDrawdown;
  final String roundTrips;
}
