import 'package:omni_folio_client/core/data/json_codec.dart';

class LedgerActivityPage {
  const LedgerActivityPage({
    required this.source,
    required this.brokerFreshness,
    required this.ledgerRevision,
    required this.recordedAt,
    required this.events,
    required this.nextCursor,
  });

  final String source;
  final String brokerFreshness;
  final String ledgerRevision;
  final String recordedAt;
  final List<LedgerActivity> events;
  final String? nextCursor;

  void requireContinuationOf({
    required LedgerActivityPage initial,
    required String upperBoundOccurredAt,
    required String requestedCursor,
    String? lowerBoundOccurredAt,
    Iterable<String> traversedCursors = const [],
  }) {
    final continuationCursor = nextCursor;
    if (events.isEmpty ||
        ledgerRevision != initial.ledgerRevision ||
        recordedAt != initial.recordedAt ||
        (continuationCursor != null &&
            (continuationCursor == requestedCursor ||
                traversedCursors.contains(continuationCursor))) ||
        jsonUtcNanoseconds(
              events.first.occurredAt,
            ).compareTo(jsonUtcNanoseconds(upperBoundOccurredAt)) >
            0 ||
        (lowerBoundOccurredAt != null &&
            jsonUtcNanoseconds(
                  events.last.occurredAt,
                ).compareTo(jsonUtcNanoseconds(lowerBoundOccurredAt)) <
                0)) {
      throw const FormatException('Invalid ledger activity continuation');
    }
  }
}

class InstrumentLedger {
  const InstrumentLedger({
    required this.instrumentId,
    required this.symbol,
    required this.currency,
    required this.ledgerRevision,
    required this.ledgerAsOf,
    required this.ledgerRecordedAt,
    required this.quantity,
    required this.costBasis,
    required this.averageCost,
    required this.hasMore,
    required this.trades,
  });

  final String instrumentId, symbol, currency;
  final String ledgerRevision, ledgerAsOf, ledgerRecordedAt;
  final String quantity, costBasis;
  final String? averageCost;
  final bool hasMore;
  final List<LedgerActivity> trades;
}

class LedgerActivity {
  const LedgerActivity({
    required this.type,
    required this.occurredAt,
    required this.recordedAt,
    required this.symbol,
    required this.quantity,
    required this.price,
    required this.fee,
    required this.currency,
    required this.amount,
    required this.counterCurrency,
    required this.counterAmount,
    required this.isCorrection,
  });

  final String type;
  final String occurredAt;
  final String recordedAt;
  final String? symbol;
  final String? quantity;
  final String? price;
  final String? fee;
  final String currency;
  final String amount;
  final String? counterCurrency;
  final String? counterAmount;
  final bool isCorrection;
}

class ImportPreview {
  const ImportPreview({
    required this.previewId,
    required this.fileSha256,
    required this.schemaVersion,
    required this.mappingVersion,
    required this.ledgerRevision,
    required this.previewFingerprint,
    required this.canApply,
    required this.totalRows,
    required this.newRows,
    required this.duplicateRows,
    required this.errorRows,
    required this.unresolvedRows,
    required this.rows,
  });

  final String previewId;
  final String fileSha256;
  final String schemaVersion;
  final String mappingVersion;
  final String ledgerRevision;
  final String previewFingerprint;
  final bool canApply;
  final int totalRows;
  final int newRows;
  final int duplicateRows;
  final int errorRows;
  final int unresolvedRows;
  final List<PreviewRow> rows;
}

class PreviewRow {
  const PreviewRow({
    required this.rowNumber,
    required this.status,
    this.symbol,
    this.transactionType,
    this.currency,
    this.amount,
    this.counterCurrency,
    this.counterAmount,
    this.correctionTarget,
    this.errors = const [],
  });

  final int rowNumber;
  final String status;
  final String? symbol;
  final String? transactionType;
  final String? currency;
  final String? amount;
  final String? counterCurrency;
  final String? counterAmount;
  final CorrectionTarget? correctionTarget;
  final List<String> errors;
}

class CorrectionTarget {
  const CorrectionTarget({
    required this.sourceEventId,
    required this.type,
    required this.currency,
    required this.amount,
  });

  final String sourceEventId;
  final String type;
  final String currency;
  final String amount;
}

class ApplyReceipt {
  const ApplyReceipt({
    required this.receiptId,
    required this.appliedRows,
    required this.skippedDuplicateRows,
    required this.ledgerRevisionAfter,
    required this.recordedAt,
  });

  final String receiptId;
  final int appliedRows;
  final int skippedDuplicateRows;
  final String ledgerRevisionAfter;
  final DateTime recordedAt;
}
