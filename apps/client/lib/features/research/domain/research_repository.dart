import 'package:omni_folio_client/features/research/domain/public_research.dart';
import 'package:omni_folio_client/features/research/domain/research_report.dart';

abstract interface class ResearchRepository {
  Future<ResearchReport> researchReport();
  Future<PublicResearch> publicResearch(PublicResearchIdentity identity);
}
