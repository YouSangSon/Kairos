import 'package:omni_folio_client/features/paper/domain/paper_monitor.dart';
import 'package:omni_folio_client/features/paper/domain/paper_promotion_report.dart';

abstract interface class PaperRepository {
  Future<PaperMonitor> paperMonitor();
  Future<PaperPromotionReport> paperPromotionReport();
}
