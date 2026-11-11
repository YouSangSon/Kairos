import 'package:omni_folio_client/features/portfolio/domain/models.dart';

abstract interface class PortfolioRepository {
  Future<PortfolioSnapshot> snapshot();
  Future<CurrentPortfolioValuation> currentPortfolioValuation();
  Future<PortfolioXirr> portfolioXirr();
  Future<PortfolioTwr> portfolioTwr();
  Future<PortfolioTwrCheckpoints> portfolioTwrCheckpoints();
  Future<PortfolioBenchmarks> portfolioBenchmarks();
  Future<HoldingValuation> holdingValuation();
}
