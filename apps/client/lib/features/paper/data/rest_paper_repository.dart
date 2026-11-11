import 'dart:async';
import 'package:omni_folio_client/core/data/http_transport.dart';
import 'package:omni_folio_client/features/paper/data/paper_monitor_codec.dart';
import 'package:omni_folio_client/features/paper/data/paper_promotion_report_codec.dart';
import 'package:omni_folio_client/features/paper/domain/paper_monitor.dart';
import 'package:omni_folio_client/features/paper/domain/paper_promotion_report.dart';
import 'package:omni_folio_client/features/paper/domain/paper_repository.dart';

class RestPaperRepository implements PaperRepository {
  RestPaperRepository(this.transport);
  final HttpTransport transport;

  @override
  Future<PaperMonitor> paperMonitor() async =>
      decodePaperMonitor(await transport.get('/v1/paper/monitor'));

  @override
  Future<PaperPromotionReport> paperPromotionReport() async =>
      decodePaperPromotionReport(
        await transport.get('/v1/paper/promotion-report'),
      );
}
