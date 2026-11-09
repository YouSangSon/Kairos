import 'package:omni_folio_client/features/market_data/domain/models.dart';

abstract interface class MarketDataRepository {
  Future<MarketCandles> candles(String symbol);
  Future<OwnerDataPreview> previewOwnerData(String input);
  Future<OwnerDataReceipt> applyOwnerData(OwnerDataPreview review);
}
