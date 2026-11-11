import 'package:omni_folio_client/features/ledger/domain/models.dart';

abstract interface class LedgerRepository {
  Future<InstrumentLedger> instrumentLedger(
    String instrumentId,
    String currency,
  );
  Future<LedgerActivityPage> ledgerActivities({String? cursor});
  Future<ImportPreview> preview(String csv);
  Future<ApplyReceipt> apply(String previewId, String idempotencyKey);
}
