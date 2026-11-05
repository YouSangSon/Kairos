class PublicResearchIdentity {
  PublicResearchIdentity({required this.market, required this.symbol}) {
    if (!const {'KR', 'US'}.contains(market) ||
        RegExp(r'^[A-Z0-9][A-Z0-9.\-]{0,31}$').stringMatch(symbol) != symbol) {
      throw const FormatException('Invalid public research identity');
    }
  }

  factory PublicResearchIdentity.fromVenue(String venue, String symbol) =>
      PublicResearchIdentity(
        market: switch (venue) {
          'XKRX' => 'KR',
          'XNAS' || 'XNYS' => 'US',
          _ => throw const FormatException('Unsupported research venue'),
        },
        symbol: symbol,
      );

  final String market, symbol;

  bool matches(PublicResearchIdentity other) =>
      market == other.market && symbol == other.symbol;
}

class PublicResearch {
  PublicResearch({
    required this.identity,
    required this.observedAt,
    required this.generatedAt,
    required this.since,
    required this.until,
    required this.sample,
    required this.unscopedNews,
    required this.missingSourceEvidence,
    required this.items,
  });

  final PublicResearchIdentity identity;
  final String observedAt, generatedAt, since, until;
  final bool sample;
  final int unscopedNews, missingSourceEvidence;
  final List<PublicResearchItem> items;
}

class PublicResearchItem {
  PublicResearchItem(
    this.id,
    this.kind,
    this.identity,
    this.source,
    this.occurredAt,
    this.capturedAt,
    this.sources,
    this.title,
    this.summary,
    this.url,
    this.association,
    this.asOf,
    this.direction,
    this.stars,
    this.confidence,
    this.reasons,
    this.disclaimer,
  );

  final String id, kind, source, occurredAt, capturedAt;
  final PublicResearchIdentity identity;
  final List<ResearchSourceEvidence> sources;
  final String? title, summary, url, association, asOf, direction, disclaimer;
  final int? stars;
  final double? confidence;
  final List<String> reasons;
}

class ResearchSourceEvidence {
  ResearchSourceEvidence(
    this.publisher,
    this.target,
    this.sourceUrl,
    this.evidence,
    this.attribution,
    this.author,
    this.rightsHolder,
    this.rightsSourceUrl,
    this.checkedOn,
    this.termsUrls,
  );

  final String publisher, target, sourceUrl, evidence;
  final String? attribution, author, rightsHolder, rightsSourceUrl, checkedOn;
  final List<String> termsUrls;
}
