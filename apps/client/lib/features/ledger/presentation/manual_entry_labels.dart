import 'package:omni_folio_client/features/ledger/domain/manual_entry.dart';

const manualEntryTypes = {
  'BUY': '매수 기록',
  'SELL': '매도 기록',
  'DEPOSIT': '입금',
  'WITHDRAWAL': '출금',
  'DIVIDEND': '배당',
  'FEE': '수수료',
  'TAX': '세금',
  'SPLIT': '주식 분할',
  'FX_EXCHANGE': '환전',
  'CASH_VOID': '현금 기록 취소',
};

String manualEntryLabel(String type, String field) => switch (field) {
  'occurred_at' => '거래 시각 (시간대 포함)',
  'currency' => type == 'FX_EXCHANGE' ? '내보내는 통화' : '통화',
  'symbol' => '종목 코드',
  'quantity' => type == 'SPLIT' ? '분할 배율' : '수량',
  'price' => '단가',
  'fee' => '수수료',
  'amount' => type == 'CASH_VOID' ? '취소 금액 (부호 포함)' : '금액 (양수)',
  'counter_currency' => '받는 통화',
  'counter_amount' => '받는 금액 (양수)',
  'corrects_source_event_id' => '취소할 기록 구분값',
  _ => field,
};

Map<String, String> manualEntryErrors(
  String type,
  Map<String, String> fields,
) => validateManualEntry(type, fields).map(
  (field, error) => MapEntry(field, switch (error) {
    ManualEntryError.unsupportedType => '지원하는 기록 유형을 선택하세요.',
    ManualEntryError.timestamp => '유효한 날짜·시간대와 최대 9자리 소수초를 입력하세요.',
    ManualEntryError.currency => 'USD 또는 KRW처럼 영문 3자리로 입력하세요.',
    ManualEntryError.nonzero => '0이 아닌 금액이나 수량을 입력하세요.',
    ManualEntryError.signedDecimal => '구분자 없이 부호·숫자·소수점으로 입력하세요.',
    ManualEntryError.positiveDecimal => '구분자 없이 양수와 소수점으로 입력하세요.',
    ManualEntryError.differentCurrency => '내보내는 통화와 다른 통화를 입력하세요.',
    ManualEntryError.length128 => '값을 128자 이내로 입력하세요.',
    ManualEntryError.length64 => '값을 64자 이내로 입력하세요.',
  }),
);
