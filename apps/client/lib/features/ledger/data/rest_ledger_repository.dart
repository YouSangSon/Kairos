import 'dart:async';
import 'dart:convert';
import 'package:omni_folio_client/core/data/http_transport.dart';
import 'package:omni_folio_client/features/ledger/data/codecs.dart';
import 'package:omni_folio_client/features/ledger/domain/ledger_repository.dart';
import 'package:omni_folio_client/features/ledger/domain/models.dart';

class RestLedgerRepository implements LedgerRepository {
  RestLedgerRepository(this.transport);
  final HttpTransport transport;

  @override
  Future<InstrumentLedger> instrumentLedger(
    String instrumentId,
    String currency,
  ) async {
    final view = decodeInstrumentLedger(
      await transport.getUri(
        transport
            .uri('/v1/portfolio/instrument-ledger')
            .replace(
              queryParameters: {
                'instrument_id': instrumentId,
                'currency': currency,
              },
            ),
      ),
    );
    if (view.instrumentId != instrumentId || view.currency != currency) {
      throw const FormatException('Instrument ledger does not match request');
    }
    return view;
  }

  @override
  Future<LedgerActivityPage> ledgerActivities({String? cursor}) async {
    final uri = transport
        .uri('/v1/ledger/activities')
        .replace(queryParameters: cursor == null ? null : {'cursor': cursor});
    return decodeLedgerActivityPage(await transport.getUri(uri));
  }

  @override
  Future<ImportPreview> preview(String csv) async {
    return decodeImportPreview(
      await transport.post(
        '/v1/imports/preview',
        contentType: 'text/csv',
        body: csv,
      ),
    );
  }

  @override
  Future<ApplyReceipt> apply(String previewId, String idempotencyKey) async {
    return decodeApplyReceipt(
      await transport.post(
        '/v1/imports/apply',
        contentType: 'application/json',
        body: jsonEncode({
          'preview_id': previewId,
          'idempotency_key': idempotencyKey,
        }),
      ),
    );
  }
}
