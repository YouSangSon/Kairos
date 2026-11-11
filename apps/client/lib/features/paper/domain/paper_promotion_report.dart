class PaperPromotionReport {
  const PaperPromotionReport({required this.observedAt, required this.attempt});

  final String observedAt;
  final PaperPromotionAttempt? attempt;
}

class PaperPromotionAttempt {
  const PaperPromotionAttempt({
    required this.symbol,
    required this.claimedAt,
    required this.decidedAt,
    required this.comparisonStart,
    required this.comparisonEnd,
    required this.candidate,
    required this.incumbent,
    required this.decision,
    required this.reasonCode,
    required this.selectionStatus,
    required this.comparison,
  });

  final String symbol;
  final String claimedAt;
  final String? decidedAt;
  final String comparisonStart;
  final String comparisonEnd;
  final PaperPromotionStrategy candidate;
  final PaperPromotionStrategy incumbent;
  final String decision;
  final String reasonCode;
  final String selectionStatus;
  final PaperPromotionComparison? comparison;
}

class PaperPromotionStrategy {
  const PaperPromotionStrategy({
    required this.fastWindow,
    required this.slowWindow,
    required this.quantity,
  });

  final int fastWindow;
  final int slowWindow;
  final String quantity;
}

class PaperPromotionComparison {
  const PaperPromotionComparison({
    required this.decision,
    required this.reasonCode,
    required this.windows,
  });

  final String decision;
  final String reasonCode;
  final List<PaperPromotionWindow> windows;
}

class PaperPromotionWindow {
  const PaperPromotionWindow({
    required this.start,
    required this.end,
    required this.candidate,
    required this.incumbent,
    required this.buyAndHold,
  });

  final String start;
  final String end;
  final PaperPromotionMetrics candidate;
  final PaperPromotionMetrics incumbent;
  final PaperPromotionMetrics buyAndHold;
}

class PaperPromotionMetrics {
  const PaperPromotionMetrics({
    required this.afterCostReturn,
    required this.maxDrawdown,
    required this.roundTrips,
    required this.turnover,
  });

  final String afterCostReturn;
  final String maxDrawdown;
  final String roundTrips;
  final String turnover;
}
