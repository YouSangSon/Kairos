import 'dart:convert';
import 'package:omni_folio_client/core/data/codecs.dart';
import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/core/domain/decimal.dart';
import 'package:omni_folio_client/core/domain/models.dart';
import 'package:omni_folio_client/features/portfolio/domain/models.dart';

Holding decodeHolding(Json json) => Holding(
  instrumentId: jsonText(json, 'instrument_id'),
  symbol: jsonText(json, 'symbol'),
  quantity: jsonDecimal(json, 'quantity'),
  costBasis: jsonDecimal(json, 'cost_basis'),
  currency: jsonText(json, 'currency'),
);

HoldingValuationIssue decodeHoldingValuationIssue(Json json) {
  final keys = json.keys.toSet();
  if (keys.length < 2 ||
      keys.length > 3 ||
      !keys.containsAll(const {'code', 'message'}) ||
      !keys.every(const {'code', 'message', 'field'}.contains)) {
    throw const FormatException('Unexpected valuation issue fields');
  }
  return HoldingValuationIssue(
    code: jsonText(json, 'code'),
    message: jsonText(json, 'message'),
    field: jsonNullableText(json, 'field'),
  );
}

HoldingValuationPrice decodeHoldingValuationPrice(Json json) {
  final owner = json['source'] == 'owner_declared';
  jsonRequireExactKeys(json, {
    'source',
    'venue',
    'currency',
    'price',
    'price_adjustment',
    'observed_at',
    'fetched_at',
    'recorded_at',
    'sample',
    'state',
    if (owner) 'source_version',
  });
  final price = jsonDecimal(json, 'price');
  final source = jsonText(json, 'source');
  final adjustment = jsonText(json, 'price_adjustment');
  final sample = jsonBool(json, 'sample');
  final state = jsonText(json, 'state');
  final venue = jsonText(json, 'venue');
  if (price == '0' ||
      price.startsWith('-') ||
      (owner
          ? price.length > 64 ||
                adjustment != 'unadjusted' ||
                !const {'unknown', 'stale'}.contains(state)
          : source != 'local_fixture' ||
                adjustment != 'unspecified' ||
                !sample ||
                state != 'stale')) {
    throw const FormatException('Invalid holding price provenance');
  }
  if (owner &&
      (jsonBenchmarkText(json, 'venue', 128, identity: true) != venue ||
          venue != venue.toUpperCase())) {
    throw const FormatException('Invalid owner price venue');
  }
  return HoldingValuationPrice(
    source: source,
    sourceVersion: owner
        ? jsonBenchmarkText(json, 'source_version', 128, identity: true)
        : null,
    venue: venue,
    currency: jsonCurrency(json, 'currency'),
    price: price,
    priceAdjustment: adjustment,
    observedAt: jsonUtcText(json, 'observed_at'),
    fetchedAt: jsonUtcText(json, 'fetched_at'),
    recordedAt: jsonUtcText(json, 'recorded_at'),
    sample: sample,
    state: state,
  );
}

HoldingValuationLine decodeHoldingValuationLine(Json json) {
  jsonRequireExactKeys(json, const {
    'symbol',
    'quantity',
    'cost_basis',
    'currency',
    'status',
    'price',
    'market_value',
    'unrealized_pnl',
    'issue',
  });
  final quantity = jsonDecimal(json, 'quantity');
  final costBasis = jsonDecimal(json, 'cost_basis');
  final status = jsonText(json, 'status');
  final priceJson = json['price'];
  final issueJson = json['issue'];
  if (priceJson != null && priceJson is! Json ||
      issueJson != null && issueJson is! Json) {
    throw const FormatException('Invalid holding valuation object');
  }
  final price = priceJson is Json
      ? decodeHoldingValuationPrice(priceJson)
      : null;
  final marketValue = jsonNullableDecimal(json, 'market_value');
  final unrealizedPnl = jsonNullableDecimal(json, 'unrealized_pnl');
  final issue = issueJson is Json
      ? decodeHoldingValuationIssue(issueJson)
      : null;
  final valued = status == 'valued';
  final stale = status == 'stale';
  final splitBasis = status == 'split_basis';
  final expectedIssueCode = switch (status) {
    'missing' => 'missing_security_price',
    'ambiguous' => 'ambiguous_security_price_venue',
    'stale' => 'stale_security_price',
    'split_basis' => 'split_price_unavailable',
    _ => null,
  };
  if (!const {
        'valued',
        'missing',
        'ambiguous',
        'stale',
        'split_basis',
      }.contains(status) ||
      quantity == '0' ||
      quantity.startsWith('-') ||
      costBasis.startsWith('-') ||
      (marketValue != null &&
          (marketValue == '0' || marketValue.startsWith('-'))) ||
      (valued !=
          (price != null &&
              marketValue != null &&
              unrealizedPnl != null &&
              issue == null)) ||
      (!valued && (marketValue != null || unrealizedPnl != null)) ||
      ((valued || stale || splitBasis) != (price != null)) ||
      (splitBasis && price?.source != 'owner_declared') ||
      (!valued &&
          (issue == null ||
              issue.code != expectedIssueCode ||
              issue.field != jsonText(json, 'symbol')))) {
    throw const FormatException('Inconsistent holding valuation line');
  }
  return HoldingValuationLine(
    symbol: jsonText(json, 'symbol'),
    quantity: quantity,
    costBasis: costBasis,
    currency: jsonCurrency(json, 'currency'),
    status: status,
    price: price,
    marketValue: marketValue,
    unrealizedPnl: unrealizedPnl,
    issue: issue,
  );
}

HoldingValuationTotal decodeHoldingValuationTotal(Json json) {
  jsonRequireExactKeys(json, const {
    'currency',
    'cost_basis',
    'market_value',
    'unrealized_pnl',
  });
  final costBasis = jsonDecimal(json, 'cost_basis');
  final marketValue = jsonDecimal(json, 'market_value');
  if (costBasis.startsWith('-') ||
      marketValue == '0' ||
      marketValue.startsWith('-')) {
    throw const FormatException('Invalid holding valuation total');
  }
  return HoldingValuationTotal(
    currency: jsonCurrency(json, 'currency'),
    costBasis: costBasis,
    marketValue: marketValue,
    unrealizedPnl: jsonDecimal(json, 'unrealized_pnl'),
  );
}

HoldingValuation decodeHoldingValuation(Json json) {
  jsonRequireExactKeys(json, const {
    'scope',
    'policy_version',
    'max_observation_age_seconds',
    'valuation_as_of',
    'ledger_revision',
    'ledger_as_of',
    'ledger_recorded_at',
    'status',
    'sample',
    'totals',
    'lines',
    'issues',
  });
  final maxAge = jsonCount(json, 'max_observation_age_seconds');
  final status = jsonText(json, 'status');
  final sample = jsonBool(json, 'sample');
  final valuationAsOf = jsonUtcText(json, 'valuation_as_of');
  final ledgerAsOf = jsonUtcText(json, 'ledger_as_of');
  final ledgerRecordedAt = jsonUtcText(json, 'ledger_recorded_at');
  final totalsValue = json['totals'];
  if (totalsValue != null && totalsValue is! List<dynamic>) {
    throw const FormatException('Invalid holding valuation totals');
  }
  final totals = totalsValue == null
      ? const <HoldingValuationTotal>[]
      : jsonJsonList(
          json,
          'totals',
        ).map(decodeHoldingValuationTotal).toList(growable: false);
  final lines = jsonJsonList(
    json,
    'lines',
  ).map(decodeHoldingValuationLine).toList(growable: false);
  final issues = jsonJsonList(
    json,
    'issues',
  ).map(decodeHoldingValuationIssue).toList(growable: false);
  final ownerIssues = issues
      .where((i) => i.code == 'owner_declared_price')
      .toList();
  final owner = ownerIssues.isNotEmpty;
  final calculationIssues = issues
      .where((i) => i.code != 'owner_declared_price')
      .toList();
  if (ownerIssues.length > 1 ||
      ownerIssues.any((i) => i.field != null) ||
      owner != lines.any((l) => l.price?.source == 'owner_declared')) {
    throw const FormatException('Invalid holding price authority');
  }
  final valuationTime = jsonUtcNanoseconds(valuationAsOf);
  if (jsonText(json, 'scope') != 'holdings_only' ||
      jsonText(json, 'policy_version') !=
          (owner
              ? 'native_holding_valuation_v2'
              : 'native_holding_valuation_v1') ||
      maxAge != 86400 ||
      !const {
        'empty',
        'unavailable',
        'stale_sample',
        'complete',
      }.contains(status) ||
      !RegExp(r'^rev_[0-9]{10}$').hasMatch(jsonText(json, 'ledger_revision')) ||
      jsonUtcNanoseconds(ledgerAsOf) > valuationTime ||
      jsonUtcNanoseconds(ledgerRecordedAt) > valuationTime) {
    throw const FormatException('Invalid holding valuation metadata');
  }
  for (final line in lines) {
    final price = line.price;
    if (price == null) continue;
    final observedAt = jsonUtcNanoseconds(price.observedAt);
    final fetchedAt = jsonUtcNanoseconds(price.fetchedAt);
    final recordedAt = jsonUtcNanoseconds(price.recordedAt);
    final age = valuationTime - observedAt;
    final maxAge = BigInt.from(86400) * BigInt.from(1000000000);
    if (price.currency != line.currency ||
        fetchedAt < observedAt ||
        recordedAt < fetchedAt ||
        recordedAt > valuationTime ||
        (line.status == 'valued' && age > maxAge) ||
        (line.status == 'stale' && age <= maxAge) ||
        (price.source == 'owner_declared' &&
            price.state != (age > maxAge ? 'stale' : 'unknown'))) {
      throw const FormatException('Inconsistent holding price timing');
    }
  }
  final unvalued = lines.where((line) => line.status != 'valued').toList();
  final validState = switch (status) {
    'empty' =>
      lines.isEmpty && totalsValue == null && !sample && issues.isEmpty,
    'unavailable' =>
      lines.isNotEmpty &&
          unvalued.isNotEmpty &&
          totalsValue == null &&
          calculationIssues.length == unvalued.length &&
          _sameValuationIssues(unvalued, calculationIssues),
    'complete' =>
      lines.isNotEmpty &&
          unvalued.isEmpty &&
          totals.isNotEmpty &&
          !sample &&
          owner &&
          calculationIssues.isEmpty,
    'stale_sample' =>
      lines.isNotEmpty &&
          unvalued.isEmpty &&
          totals.isNotEmpty &&
          sample &&
          calculationIssues.length == 1 &&
          calculationIssues.single.code == 'sample_data' &&
          calculationIssues.single.field == null,
    _ => false,
  };
  if (!validState ||
      sample != lines.any((line) => line.price?.sample == true)) {
    throw const FormatException('Inconsistent holding valuation state');
  }
  if (status == 'stale_sample' || status == 'complete') {
    var previousCurrency = '';
    final totalCurrencies = <String>{};
    for (final total in totals) {
      if (total.currency.compareTo(previousCurrency) <= 0) {
        throw const FormatException('Valuation totals must be ordered');
      }
      previousCurrency = total.currency;
      totalCurrencies.add(total.currency);
    }
    if (!totalCurrencies.containsAll(lines.map((line) => line.currency)) ||
        !lines
            .map((line) => line.currency)
            .toSet()
            .containsAll(totalCurrencies)) {
      throw const FormatException('Valuation total currencies drifted');
    }
  }
  return HoldingValuation(
    valuationAsOf: valuationAsOf,
    ledgerRevision: jsonText(json, 'ledger_revision'),
    ledgerAsOf: ledgerAsOf,
    ledgerRecordedAt: ledgerRecordedAt,
    status: status,
    sample: sample,
    totals: totals,
    lines: lines,
    issues: issues,
  );
}

CurrentPortfolioValuation decodeCurrentPortfolioValuation(Json json) {
  jsonRequireExactKeys(json, const {
    'scope',
    'policy_version',
    'max_observation_age_seconds',
    'base_currency',
    'valuation_as_of',
    'ledger_revision',
    'ledger_as_of',
    'ledger_recorded_at',
    'status',
    'sample',
    'cash_value',
    'holdings_value',
    'total_value',
    'issues',
  });
  final maxAge = jsonCount(json, 'max_observation_age_seconds');
  final baseCurrency = jsonCurrency(json, 'base_currency');
  final valuationAsOf = jsonUtcText(json, 'valuation_as_of');
  final ledgerAsOf = jsonUtcText(json, 'ledger_as_of');
  final ledgerRecordedAt = jsonUtcText(json, 'ledger_recorded_at');
  final ledgerRevision = jsonText(json, 'ledger_revision');
  final status = jsonText(json, 'status');
  final sample = jsonBool(json, 'sample');
  final cashValue = _currentValuationMoney(json, 'cash_value');
  final holdingsValue = _currentValuationMoney(json, 'holdings_value');
  final totalValue = _currentValuationMoney(json, 'total_value');
  final issues = jsonJsonList(
    json,
    'issues',
  ).map(decodeHoldingValuationIssue).toList(growable: false);
  final ownerIssues = issues
      .where((i) => i.code == 'owner_declared_fx')
      .toList();
  if (ownerIssues.length > 1 || ownerIssues.any((i) => i.field != null)) {
    throw const FormatException('Invalid declared FX notice');
  }
  final owner = ownerIssues.isNotEmpty;
  final priceIssues = issues
      .where((i) => i.code == 'owner_declared_price')
      .toList();
  if (priceIssues.length > 1 || priceIssues.any((i) => i.field != null)) {
    throw const FormatException('Invalid owner price notice');
  }
  final ownerPrice = priceIssues.isNotEmpty;
  final calculationIssues = issues
      .where(
        (i) =>
            i.code != 'owner_declared_fx' && i.code != 'owner_declared_price',
      )
      .toList();
  if (jsonText(json, 'scope') != 'whole_portfolio' ||
      jsonText(json, 'policy_version') !=
          (ownerPrice
              ? 'current_base_portfolio_valuation_v3'
              : owner
              ? 'current_base_portfolio_valuation_v2'
              : 'current_base_portfolio_valuation_v1') ||
      maxAge != 86400 ||
      baseCurrency != 'KRW' ||
      !RegExp(r'^rev_[0-9]{10}$').hasMatch(ledgerRevision) ||
      jsonUtcNanoseconds(ledgerAsOf) > jsonUtcNanoseconds(valuationAsOf) ||
      jsonUtcNanoseconds(ledgerRecordedAt) >
          jsonUtcNanoseconds(valuationAsOf) ||
      !const {
        'empty',
        'complete',
        'stale_sample',
        'unavailable',
      }.contains(status)) {
    throw const FormatException('Invalid current valuation metadata');
  }
  final values = [cashValue, holdingsValue, totalValue];
  final allValues = values.every((value) => value != null);
  final noValues = values.every((value) => value == null);
  if (!allValues && !noValues) {
    throw const FormatException('Current valuation must be all-or-none');
  }
  if (allValues) {
    if (values.any((value) => value!.currency != baseCurrency) ||
        holdingsValue!.amount.startsWith('-') ||
        !decimalSumEquals(
          cashValue!.amount,
          holdingsValue.amount,
          totalValue!.amount,
        )) {
      throw const FormatException('Invalid current valuation aggregate');
    }
  }
  final issueKeys = <String>{};
  final validCoverageIssues = calculationIssues.every((issue) {
    final key = '${issue.code}:${issue.field}';
    return const {
          'missing_direct_fx',
          'stale_direct_fx',
          'missing_security_price',
          'ambiguous_security_price_venue',
          'stale_security_price',
          'split_price_unavailable',
        }.contains(issue.code) &&
        (issue.code != 'split_price_unavailable' || ownerPrice) &&
        issue.field != null &&
        issueKeys.add(key);
  });
  final validState = switch (status) {
    'empty' => noValues && !sample && issues.isEmpty,
    'complete' =>
      allValues &&
          (ownerPrice
              ? holdingsValue!.amount != '0'
              : holdingsValue!.amount == '0') &&
          !sample &&
          calculationIssues.isEmpty,
    'stale_sample' =>
      allValues &&
          sample &&
          calculationIssues.length == 1 &&
          calculationIssues.single.code == 'sample_data' &&
          calculationIssues.single.field == null,
    'unavailable' =>
      noValues && calculationIssues.isNotEmpty && validCoverageIssues,
    _ => false,
  };
  if (!validState ||
      (ownerPrice && allValues && holdingsValue!.amount == '0')) {
    throw const FormatException('Inconsistent current valuation state');
  }
  return CurrentPortfolioValuation(
    baseCurrency: baseCurrency,
    valuationAsOf: valuationAsOf,
    ledgerRevision: ledgerRevision,
    ledgerAsOf: ledgerAsOf,
    ledgerRecordedAt: ledgerRecordedAt,
    status: status,
    sample: sample,
    cashValue: cashValue,
    holdingsValue: holdingsValue,
    totalValue: totalValue,
    issues: issues,
  );
}

Money? _currentValuationMoney(Json json, String key) {
  final value = json[key];
  if (value == null) return null;
  if (value is! Json) {
    throw const FormatException('Invalid current valuation money');
  }
  jsonRequireExactKeys(value, const {'currency', 'amount'});
  return Money(jsonCurrency(value, 'currency'), jsonDecimal(value, 'amount'));
}

bool _sameValuationIssues(
  List<HoldingValuationLine> lines,
  List<HoldingValuationIssue> issues,
) {
  for (var index = 0; index < lines.length; index += 1) {
    final lineIssue = lines[index].issue!;
    final issue = issues[index];
    if (lineIssue.code != issue.code || lineIssue.field != issue.field) {
      return false;
    }
  }
  return true;
}

PortfolioSnapshot decodePortfolioSnapshot(Json json) {
  if (jsonBool(json, 'live_enabled') ||
      jsonText(json, 'valuation_status') != 'unavailable') {
    throw const FormatException('Live valuation is not supported');
  }
  if (jsonText(json, 'cost_basis_policy') !=
      'fifo_utc_nanosecond_exact_else_half_even_residual_8_v2') {
    throw const FormatException('Unsupported cost basis policy');
  }
  final provenance = json['provenance'];
  if (provenance is! Json) {
    throw const FormatException('Missing provenance');
  }
  jsonRequireExactKeys(provenance, {'event_ids', 'receipt_ids'});
  return PortfolioSnapshot(
    ledgerRevision: jsonText(json, 'ledger_revision'),
    costBasisPolicy: jsonText(json, 'cost_basis_policy'),
    recordedAt: DateTime.parse(jsonText(json, 'recorded_at')),
    cash: jsonMoneyList(json, 'cash'),
    holdings: jsonJsonList(
      json,
      'holdings',
    ).map(decodeHolding).toList(growable: false),
    realizedPnl: jsonMoneyList(json, 'realized_pnl'),
    eventCount: jsonNonEmptyStringCount(provenance, 'event_ids'),
    receiptCount: jsonNonEmptyStringCount(provenance, 'receipt_ids'),
  );
}

PortfolioTwrCheckpoints decodePortfolioTwrCheckpoints(Json json) {
  jsonRequireExactKeys(json, {'policy_version', 'report', 'points'});
  if (json['policy_version'] != 'twr_cash_flow_checkpoints.v1' ||
      json['report'] is! Json) {
    throw const FormatException('Unsupported TWR checkpoints');
  }
  final report = decodePortfolioTwr(json['report'] as Json);
  final raw = jsonJsonList(json, 'points');
  if (report.status != 'calculated') {
    if (raw.isNotEmpty) {
      throw const FormatException(
        'Uncalculated report has partial checkpoints',
      );
    }
    return PortfolioTwrCheckpoints(report, []);
  }
  final count = report.boundaryCount;
  if (count == null || count < 1 || count > 1000 || raw.length != count + 1) {
    throw const FormatException('Checkpoint count differs from TWR boundaries');
  }
  final points = <TwrCheckpoint>[];
  BigInt? previous;
  for (var index = 0; index < raw.length; index++) {
    final value = raw[index];
    jsonRequireExactKeys(value, {'at', 'kind', 'cumulative_rate'});
    final at = jsonUtcText(value, 'at');
    final time = jsonUtcNanoseconds(at);
    final rate = jsonDecimal(value, 'cumulative_rate');
    final terminal = index == count;
    if (value['kind'] != (terminal ? 'valuation' : 'external_flow') ||
        rate.length > 2500 ||
        decimalScale(rate) > 12 ||
        (rate.startsWith('-') &&
            comparePositiveDecimal(rate.substring(1), '1') > 0) ||
        time < jsonUtcNanoseconds(report.periodStart!) ||
        time > jsonUtcNanoseconds(report.periodEnd) ||
        (previous != null && (terminal ? time < previous : time <= previous)) ||
        (index == 0 && (at != report.periodStart || rate != '0')) ||
        (terminal &&
            (at != report.periodEnd || rate != report.cumulativeRate))) {
      throw const FormatException(
        'Checkpoint phase, range or rate contradicts TWR report',
      );
    }
    points.add(
      TwrCheckpoint(at, terminal ? 'valuation' : 'external_flow', rate),
    );
    previous = time;
  }
  return PortfolioTwrCheckpoints(report, points);
}

PortfolioTwr decodePortfolioTwr(Json json) {
  jsonRequireExactKeys(json, const {
    'policy_version',
    'scope',
    'valuation',
    'status',
    'sample',
    'period_start',
    'period_end',
    'external_event_count',
    'boundary_count',
    'cumulative_rate',
    'net_contributions',
    'issues',
  });
  if (!const {
        'restated_external_first_exact_timestamp_twr_v1',
        'restated_external_first_exact_timestamp_twr_v2',
        'restated_external_first_exact_timestamp_twr_v3',
      }.contains(jsonText(json, 'policy_version')) ||
      jsonText(json, 'scope') != 'whole_portfolio_since_inception' ||
      json['valuation'] is! Json) {
    throw const FormatException('Unsupported TWR report');
  }
  final valuation = decodeCurrentPortfolioValuation(json['valuation'] as Json);
  final status = jsonText(json, 'status');
  final sample = jsonBool(json, 'sample');
  final start = json['period_start'] == null
      ? null
      : jsonUtcText(json, 'period_start');
  final end = jsonUtcText(json, 'period_end');
  final count = json['external_event_count'] == null
      ? null
      : jsonCount(json, 'external_event_count');
  final boundaries = json['boundary_count'] == null
      ? null
      : jsonCount(json, 'boundary_count');
  final rate = jsonNullableDecimal(json, 'cumulative_rate');
  if (rate != null &&
      (rate.length > 128 ||
          decimalScale(rate) > 12 ||
          (rate.startsWith('-') &&
              comparePositiveDecimal(rate.substring(1), '1') > 0))) {
    throw const FormatException('Invalid TWR rate');
  }
  Money? net;
  if (json['net_contributions'] != null) {
    if (json['net_contributions'] is! Json) {
      throw const FormatException('Invalid TWR cash');
    }
    final money = json['net_contributions'] as Json;
    jsonRequireExactKeys(money, const {'currency', 'amount'});
    net = decodeMoney(money);
    if (net.currency != 'KRW') {
      throw const FormatException('Invalid TWR currency');
    }
  }
  final issues = jsonJsonList(
    json,
    'issues',
  ).map(decodeHoldingValuationIssue).toList(growable: false);
  const codes = {
    'sample_data',
    'owner_declared_fx',
    'owner_declared_price',
    'valuation_unavailable',
    'no_external_cash_flows',
    'cash_flow_limit',
    'numerical_limit',
    'capital_path_unavailable',
    'ambiguous_split_boundary',
    'split_price_unavailable',
    'missing_security_price',
    'stale_security_price',
    'ambiguous_security_price_venue',
    'missing_historical_fx',
    'stale_historical_fx',
  };
  final seen = <String>{};
  if (issues.any(
    (issue) =>
        !codes.contains(issue.code) ||
        !seen.add(issue.code) ||
        issue.field != null,
  )) {
    throw const FormatException('Invalid TWR issue');
  }
  final missingValuation = seen.contains('valuation_unavailable');
  final owner = seen.contains('owner_declared_fx');
  final ownerPrice = issues.any((i) => i.code == 'owner_declared_price');
  if (jsonText(json, 'policy_version') !=
          (ownerPrice
              ? 'restated_external_first_exact_timestamp_twr_v3'
              : owner
              ? 'restated_external_first_exact_timestamp_twr_v2'
              : 'restated_external_first_exact_timestamp_twr_v1') ||
      (valuation.ownerDeclaredFx && !owner) ||
      (valuation.ownerDeclaredPrice && !ownerPrice)) {
    throw const FormatException('Inconsistent TWR authority');
  }
  final reasons = seen.difference(const {
    'sample_data',
    'owner_declared_fx',
    'owner_declared_price',
  });
  final reason = reasons.isEmpty ? null : reasons.first;
  final unavailableInputs = switch (reason) {
    'valuation_unavailable' => count == null && boundaries == null,
    'no_external_cash_flows' => count == 0 && boundaries == 0,
    'cash_flow_limit' => boundaries != null && boundaries > 1000,
    null => false,
    _ =>
      count != null &&
          count > 0 &&
          boundaries != null &&
          boundaries >= 1 &&
          boundaries <= 1000,
  };
  final valid = switch (status) {
    'calculated' =>
      rate != null &&
          net != null &&
          count != null &&
          count > 0 &&
          boundaries != null &&
          boundaries <= 1000 &&
          valuation.totalValue != null &&
          issues.every(
            (issue) => const {
              'sample_data',
              'owner_declared_fx',
              'owner_declared_price',
            }.contains(issue.code),
          ),
    'empty' =>
      rate == null &&
          net == null &&
          count == 0 &&
          boundaries == 0 &&
          start == null &&
          !sample &&
          issues.isEmpty &&
          valuation.status == 'empty',
    'unavailable' =>
      rate == null &&
          net == null &&
          reasons.length == 1 &&
          unavailableInputs &&
          valuation.status != 'empty',
    _ => false,
  };
  if (!valid ||
      sample != seen.contains('sample_data') ||
      (valuation.sample && !sample) ||
      end != valuation.valuationAsOf ||
      (start != null && jsonUtcNanoseconds(start) > jsonUtcNanoseconds(end)) ||
      (missingValuation &&
          (valuation.totalValue != null ||
              count != null ||
              boundaries != null ||
              start != null)) ||
      (!missingValuation &&
          status != 'empty' &&
          (valuation.totalValue == null ||
              count == null ||
              boundaries == null)) ||
      ((count == null) != (boundaries == null)) ||
      (count == null && start != null) ||
      (count == 0 && (start != null || boundaries != 0)) ||
      (count != null &&
          count > 0 &&
          (start == null ||
              boundaries == null ||
              boundaries < 1 ||
              boundaries > count)) ||
      (boundaries != null &&
          boundaries > 1000 &&
          !seen.contains('cash_flow_limit'))) {
    throw const FormatException('Inconsistent TWR report');
  }
  return PortfolioTwr(
    valuation: valuation,
    status: status,
    sample: sample,
    periodStart: start,
    periodEnd: end,
    externalEventCount: count,
    boundaryCount: boundaries,
    cumulativeRate: rate,
    netContributions: net,
    issues: List.unmodifiable(issues),
  );
}

BenchmarkSeriesSummary decodeBenchmarkSeriesSummary(Json json) {
  jsonRequireExactKeys(json, const {
    'storage_check',
    'authority',
    'benchmark_id',
    'label',
    'currency',
    'return_basis',
    'source_reference',
    'source_version',
    'sample',
    'data_sha256',
    'point_count',
    'first_observed_at',
    'last_observed_at',
    'fetched_at',
    'recorded_at',
  });
  final id = jsonBenchmarkText(json, 'benchmark_id', 128, identity: true);
  final label = jsonBenchmarkText(json, 'label', 160, trimmed: true);
  final currency = jsonText(json, 'currency');
  final basis = jsonText(json, 'return_basis');
  final reference = jsonBenchmarkText(
    json,
    'source_reference',
    512,
    identity: true,
  );
  final uri = Uri.tryParse(reference);
  final version = jsonBenchmarkText(
    json,
    'source_version',
    128,
    identity: true,
  );
  final sha = jsonText(json, 'data_sha256');
  final count = jsonCount(json, 'point_count');
  final first = jsonUtcText(json, 'first_observed_at');
  final last = jsonUtcText(json, 'last_observed_at');
  final fetched = jsonUtcText(json, 'fetched_at');
  final recorded = jsonUtcText(json, 'recorded_at');
  final firstTime = jsonUtcNanoseconds(first);
  final lastTime = jsonUtcNanoseconds(last);
  if (json['storage_check'] != 'verified' ||
      json['authority'] != 'owner_declared' ||
      !const {'KRW', 'USD'}.contains(currency) ||
      !const {
        'price_index',
        'gross_total_return',
        'net_total_return',
      }.contains(basis) ||
      !reference.startsWith('https://') ||
      uri == null ||
      uri.scheme != 'https' ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      uri.hasQuery ||
      uri.hasFragment ||
      sha.length != 64 ||
      !RegExp(r'^[0-9a-f]{64}$').hasMatch(sha) ||
      count < 1 ||
      count > 10000 ||
      firstTime > lastTime ||
      ((count == 1) != (firstTime == lastTime)) ||
      lastTime > jsonUtcNanoseconds(fetched) ||
      jsonUtcNanoseconds(fetched) > jsonUtcNanoseconds(recorded)) {
    throw const FormatException('Invalid benchmark series summary');
  }
  return BenchmarkSeriesSummary(
    id: id,
    label: label,
    currency: currency,
    returnBasis: basis,
    sourceReference: reference,
    sourceVersion: version,
    sample: jsonBool(json, 'sample'),
    pointCount: count,
    firstObservedAt: first,
    lastObservedAt: last,
    fetchedAt: fetched,
    recordedAt: recorded,
  );
}

BenchmarkRate decodeBenchmarkRate(Json json) {
  jsonRequireExactKeys(json, const {'status', 'rate', 'issue'});
  final status = jsonText(json, 'status');
  final rate = jsonNullableDecimal(json, 'rate');
  final issue = jsonNullableText(json, 'issue');
  const codes = {
    'portfolio_unavailable',
    'missing_benchmark_point',
    'stale_benchmark_point',
    'zero_benchmark_start',
    'missing_historical_fx',
    'stale_historical_fx',
    'numerical_limit',
  };
  final calculated = status == 'calculated' && rate != null && issue == null;
  final unavailable =
      status == 'unavailable' && rate == null && codes.contains(issue);
  if ((!calculated && !unavailable) ||
      (rate != null &&
          (rate.length > 128 ||
              decimalScale(rate) > 12 ||
              (rate.startsWith('-') &&
                  comparePositiveDecimal(rate.substring(1), '1') > 0)))) {
    throw const FormatException('Invalid benchmark rate');
  }
  return BenchmarkRate(status, rate, issue);
}

BenchmarkFxObservation? _benchmarkFxObservation(dynamic raw) {
  if (raw == null) return null;
  if (raw is! Json) {
    throw const FormatException('Invalid FX observation');
  }
  final owner = raw.containsKey('source');
  jsonRequireExactKeys(
    raw,
    owner
        ? {'observed_at', 'rate', 'source', 'source_version', 'sample'}
        : {'observed_at', 'rate'},
  );
  if (owner && jsonText(raw, 'source') != 'owner_declared') {
    throw const FormatException('Invalid FX source');
  }
  final point = _benchmarkObservation({
    'observed_at': raw['observed_at'],
    'rate': raw['rate'],
  }, 'rate')!;
  return (
    observedAt: point.observedAt,
    value: point.value,
    sourceVersion: owner
        ? jsonBenchmarkText(raw, 'source_version', 128, identity: true)
        : null,
    sample: owner ? jsonBool(raw, 'sample') : true,
  );
}

BenchmarkObservation? _benchmarkObservation(dynamic raw, String valueKey) {
  if (raw == null) return null;
  if (raw is! Json) {
    throw const FormatException('Invalid benchmark observation');
  }
  jsonRequireExactKeys(raw, {'observed_at', valueKey});
  final value = jsonDecimal(raw, valueKey);
  if (value.length > 64 ||
      value.startsWith('-') ||
      (valueKey == 'rate' && value == '0')) {
    throw const FormatException('Invalid benchmark observation value');
  }
  return (observedAt: jsonUtcText(raw, 'observed_at'), value: value);
}

PortfolioBenchmark decodePortfolioBenchmark(Json json, PortfolioTwr portfolio) {
  jsonRequireExactKeys(json, const {
    'series',
    'sample',
    'start',
    'end',
    'start_fx',
    'end_fx',
    'native',
    'krw',
    'difference_percentage_points',
  });
  if (json['series'] is! Json ||
      json['native'] is! Json ||
      json['krw'] is! Json) {
    throw const FormatException('Invalid benchmark comparison');
  }
  final series = decodeBenchmarkSeriesSummary(json['series'] as Json);
  final sample = jsonBool(json, 'sample');
  final start = _benchmarkObservation(json['start'], 'level');
  final end = _benchmarkObservation(json['end'], 'level');
  final startFx = _benchmarkFxObservation(json['start_fx']);
  final endFx = _benchmarkFxObservation(json['end_fx']);
  final native = decodeBenchmarkRate(json['native'] as Json);
  final krw = decodeBenchmarkRate(json['krw'] as Json);
  final difference = jsonNullableDecimal(json, 'difference_percentage_points');
  final endAt = jsonUtcNanoseconds(portfolio.periodEnd);
  final startAt = portfolio.periodStart == null
      ? null
      : jsonUtcNanoseconds(portfolio.periodStart!);
  final maxAge = BigInt.from(86400) * BigInt.from(1000000000);
  bool stale(String? observedAt, BigInt at) =>
      observedAt != null && at - jsonUtcNanoseconds(observedAt) > maxAge;
  bool equalRates() =>
      native.status == krw.status &&
      native.rate == krw.rate &&
      native.issue == krw.issue;
  if (jsonUtcNanoseconds(series.recordedAt) > endAt ||
      sample !=
          (series.sample ||
              (startFx?.sample ?? false) ||
              (endFx?.sample ?? false)) ||
      (startFx != null &&
          endFx != null &&
          (startFx.sourceVersion != endFx.sourceVersion ||
              startFx.sample != endFx.sample))) {
    throw const FormatException('Inconsistent benchmark provenance');
  }
  for (final (observedAt, at) in [
    (start?.observedAt, startAt),
    (end?.observedAt, endAt),
    (startFx?.observedAt, startAt),
    (endFx?.observedAt, endAt),
  ]) {
    if (observedAt != null &&
        (at == null || jsonUtcNanoseconds(observedAt) > at)) {
      throw const FormatException('Future benchmark observation');
    }
  }
  for (final mark in [start, end]) {
    if (mark != null &&
        (jsonUtcNanoseconds(mark.observedAt) <
                jsonUtcNanoseconds(series.firstObservedAt) ||
            jsonUtcNanoseconds(mark.observedAt) >
                jsonUtcNanoseconds(series.lastObservedAt))) {
      throw const FormatException('Benchmark observation outside its version');
    }
  }
  if (portfolio.status != 'calculated') {
    if (native.issue != 'portfolio_unavailable' ||
        !equalRates() ||
        start != null ||
        end != null ||
        startFx != null ||
        endFx != null) {
      throw const FormatException('Comparison without calculated TWR');
    }
  } else {
    if (startAt == null ||
        end == null ||
        end.observedAt != series.lastObservedAt) {
      throw const FormatException('Missing comparison period endpoint');
    }
    final pointIssue = start == null
        ? 'missing_benchmark_point'
        : stale(start.observedAt, startAt) || stale(end.observedAt, endAt)
        ? 'stale_benchmark_point'
        : start.value == '0'
        ? 'zero_benchmark_start'
        : null;
    if (pointIssue != null) {
      if (native.issue != pointIssue ||
          !equalRates() ||
          startFx != null ||
          endFx != null) {
        throw const FormatException('Inconsistent benchmark point failure');
      }
    } else {
      if (native.issue != null && native.issue != 'numerical_limit') {
        throw const FormatException('Invalid native benchmark failure');
      }
      if (series.currency == 'KRW') {
        if (!equalRates() || startFx != null || endFx != null) {
          throw const FormatException('KRW index was converted twice');
        }
      } else {
        final fxIssue = startFx == null || endFx == null
            ? 'missing_historical_fx'
            : stale(startFx.observedAt, startAt) ||
                  stale(endFx.observedAt, endAt)
            ? 'stale_historical_fx'
            : null;
        if ((fxIssue != null && krw.issue != fxIssue) ||
            (fxIssue == null &&
                krw.issue != null &&
                krw.issue != 'numerical_limit')) {
          throw const FormatException('Inconsistent benchmark FX failure');
        }
      }
    }
  }
  if (krw.rate == null) {
    if (difference != null) {
      throw const FormatException('Difference without KRW benchmark');
    }
  } else if (difference == null ||
      portfolio.cumulativeRate == null ||
      difference.length > 132 ||
      decimalScale(difference) > 10 ||
      scaledDecimal(difference, 10) !=
          scaledDecimal(portfolio.cumulativeRate!, 12) -
              scaledDecimal(krw.rate!, 12)) {
    throw const FormatException('Invalid percentage-point difference');
  }
  return PortfolioBenchmark(
    series: series,
    sample: sample,
    start: start,
    end: end,
    startFx: startFx,
    endFx: endFx,
    native: native,
    krw: krw,
    differencePercentagePoints: difference,
  );
}

PortfolioBenchmarks decodePortfolioBenchmarks(Json json) {
  jsonRequireExactKeys(json, const {
    'policy_version',
    'max_observation_age_seconds',
    'portfolio',
    'benchmarks',
    'issues',
  });
  if (!const {
        'retrospective_latest_index_24h_native_and_krw_v1',
        'retrospective_latest_index_24h_native_and_krw_v2',
        'retrospective_latest_index_24h_native_and_krw_v3',
      }.contains(json['policy_version']) ||
      jsonCount(json, 'max_observation_age_seconds') != 86400 ||
      json['portfolio'] is! Json) {
    throw const FormatException('Unsupported portfolio benchmark policy');
  }
  final portfolio = decodePortfolioTwr(json['portfolio'] as Json);
  final rawRows = jsonJsonList(json, 'benchmarks');
  final rawIssues = jsonJsonList(json, 'issues');
  if (rawRows.length > 32 || rawIssues.length > 1) {
    throw const FormatException('Benchmark response exceeds its bounds');
  }
  final rows = rawRows
      .map((row) => decodePortfolioBenchmark(row, portfolio))
      .toList(growable: false);
  final issues = rawIssues
      .map((issue) {
        jsonRequireExactKeys(issue, const {'code', 'message'});
        return decodeHoldingValuationIssue(issue);
      })
      .toList(growable: false);
  final ids = <String>{};
  final owner =
      portfolio.ownerDeclaredFx || rows.any((row) => row.ownerDeclaredFx);
  if (json['policy_version'] !=
      (portfolio.ownerDeclaredPrice
          ? 'retrospective_latest_index_24h_native_and_krw_v3'
          : owner
          ? 'retrospective_latest_index_24h_native_and_krw_v2'
          : 'retrospective_latest_index_24h_native_and_krw_v1')) {
    throw const FormatException('Inconsistent benchmark authority');
  }
  if (rows.any((row) => !ids.add(row.series.id)) ||
      (rows.isEmpty
          ? issues.length != 1 ||
                !const {
                  'no_benchmarks',
                  'no_known_benchmarks',
                  'benchmark_limit',
                }.contains(issues.single.code)
          : issues.isNotEmpty)) {
    throw const FormatException('Invalid benchmark list state');
  }
  return PortfolioBenchmarks(
    portfolio,
    List.unmodifiable(rows),
    List.unmodifiable(issues),
  );
}

PortfolioXirr decodePortfolioXirr(Json json) {
  jsonRequireExactKeys(json, const {
    'policy_version',
    'solver_policy',
    'scope',
    'valuation',
    'status',
    'sample',
    'period_start',
    'period_end',
    'external_event_count',
    'sign_changes',
    'uniqueness',
    'annual_rate',
    'candidate_rate',
    'net_contributions',
    'issues',
  });
  if (!const {
        'restated_external_cash_utc_date_actual_365_v1',
        'restated_external_cash_utc_date_actual_365_v2',
        'restated_external_cash_utc_date_actual_365_v3',
      }.contains(jsonText(json, 'policy_version')) ||
      jsonText(json, 'solver_policy') !=
          'single_change_bracket_else_log_newton_0_1_v1' ||
      jsonText(json, 'scope') != 'whole_portfolio_since_inception') {
    throw const FormatException('Unsupported XIRR policy');
  }
  if (json['valuation'] is! Json) {
    throw const FormatException('Missing XIRR valuation');
  }
  final valuation = decodeCurrentPortfolioValuation(json['valuation'] as Json);
  final status = jsonText(json, 'status');
  final sample = jsonBool(json, 'sample');
  final start = jsonNullableText(json, 'period_start');
  final end = jsonText(json, 'period_end');
  void date(String value) {
    if (!RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(value)) {
      throw const FormatException('Invalid XIRR date');
    }
    jsonUtcText({'at': '${value}T00:00:00Z'}, 'at');
  }

  date(end);
  if (start != null) date(start);
  final count = json['external_event_count'] == null
      ? null
      : jsonCount(json, 'external_event_count');
  final changes = json['sign_changes'] == null
      ? null
      : jsonCount(json, 'sign_changes');
  final rate = jsonNullableDecimal(json, 'annual_rate');
  final candidate = jsonNullableDecimal(json, 'candidate_rate');
  for (final value in [rate, candidate]) {
    if (value != null &&
        (value.length > 323 ||
            !double.parse(value).isFinite ||
            decimalScale(value) > 12 ||
            (value.startsWith('-') &&
                comparePositiveDecimal(value.substring(1), '1') >= 0))) {
      throw const FormatException('Invalid XIRR numerical rate');
    }
  }
  Money? net;
  if (json['net_contributions'] != null) {
    if (json['net_contributions'] is! Json) {
      throw const FormatException('Invalid XIRR cash');
    }
    final money = json['net_contributions'] as Json;
    jsonRequireExactKeys(money, const {'currency', 'amount'});
    net = decodeMoney(money);
    if (net.currency != 'KRW') {
      throw const FormatException('Invalid XIRR currency');
    }
  }
  final issues = jsonJsonList(
    json,
    'issues',
  ).map(decodeHoldingValuationIssue).toList(growable: false);
  const codes = {
    'sample_data',
    'owner_declared_fx',
    'owner_declared_price',
    'valuation_unavailable',
    'missing_historical_fx',
    'stale_historical_fx',
    'insufficient_period',
    'one_sided_cash_flows',
    'cash_flow_limit',
    'numerical_limit',
    'uniqueness_not_proven',
  };
  final issueKeys = <String>{};
  if (issues.any(
    (issue) =>
        !codes.contains(issue.code) ||
        !issueKeys.add('${issue.code}:${issue.field}') ||
        (issue.field != null &&
            (!const {
                  'missing_historical_fx',
                  'stale_historical_fx',
                }.contains(issue.code) ||
                !RegExp(r'^[A-Z]{3}$').hasMatch(issue.field!))),
  )) {
    throw const FormatException('Invalid XIRR issue');
  }
  final hasSample = issues.any((issue) => issue.code == 'sample_data');
  final owner = issues.any((issue) => issue.code == 'owner_declared_fx');
  final ownerPrice = issues.any((i) => i.code == 'owner_declared_price');
  if (jsonText(json, 'policy_version') !=
          (ownerPrice
              ? 'restated_external_cash_utc_date_actual_365_v3'
              : owner
              ? 'restated_external_cash_utc_date_actual_365_v2'
              : 'restated_external_cash_utc_date_actual_365_v1') ||
      (valuation.ownerDeclaredFx && !owner) ||
      (valuation.ownerDeclaredPrice && !ownerPrice)) {
    throw const FormatException('Inconsistent XIRR authority');
  }
  final incompleteFlows = issues.any(
    (issue) => const {
      'valuation_unavailable',
      'missing_historical_fx',
      'stale_historical_fx',
    }.contains(issue.code),
  );
  final unknownInputs = incompleteFlows && (net != null || changes != null);
  final invalidValuationMissing =
      issues.any((issue) => issue.code == 'valuation_unavailable') &&
      (count != null || start != null || valuation.totalValue != null);
  if (unknownInputs || invalidValuationMissing) {
    throw const FormatException(
      'Uncomputed XIRR inputs were reported as known',
    );
  }
  final unique = jsonText(json, 'uniqueness');
  final valid = switch (status) {
    'calculated' =>
      rate != null &&
          candidate == null &&
          changes == 1 &&
          unique == 'unique' &&
          issues.every(
            (issue) => const {
              'sample_data',
              'owner_declared_fx',
              'owner_declared_price',
            }.contains(issue.code),
          ),
    'candidate' =>
      rate == null &&
          candidate != null &&
          changes != null &&
          changes > 1 &&
          unique == 'not_proven' &&
          issues.any((issue) => issue.code == 'uniqueness_not_proven') &&
          issues.every(
            (issue) => const {
              'sample_data',
              'owner_declared_fx',
              'owner_declared_price',
              'uniqueness_not_proven',
            }.contains(issue.code),
          ),
    'empty' =>
      rate == null &&
          candidate == null &&
          unique == 'not_applicable' &&
          valuation.status == 'empty' &&
          count == 0 &&
          start == null &&
          net == null &&
          issues.isEmpty &&
          !sample,
    'unavailable' =>
      rate == null &&
          candidate == null &&
          unique == 'not_applicable' &&
          issues.any(
            (issue) => !const {
              'sample_data',
              'owner_declared_fx',
              'owner_declared_price',
              'uniqueness_not_proven',
            }.contains(issue.code),
          ),
    _ => false,
  };
  if (!valid ||
      sample != hasSample ||
      (valuation.sample && !sample) ||
      end != valuation.valuationAsOf.substring(0, 10) ||
      (start != null && start.compareTo(end) > 0) ||
      (count == null && start != null) ||
      (count == 0 && start != null) ||
      (count != null && count > 0 && start == null) ||
      (changes != null && changes > 9999) ||
      (count != null && changes != null && changes > count) ||
      ((rate != null || candidate != null) &&
          (valuation.totalValue == null ||
              net == null ||
              count == null ||
              count == 0 ||
              start == end))) {
    throw const FormatException('Inconsistent XIRR report');
  }
  return PortfolioXirr(
    valuation: valuation,
    status: status,
    sample: sample,
    periodStart: start,
    periodEnd: end,
    externalEventCount: count,
    signChanges: changes,
    annualRate: rate,
    candidateRate: candidate,
    netContributions: net,
    issues: List.unmodifiable(issues),
  );
}

Json decodeObject(String body) {
  final value = jsonDecode(body);
  if (value is! Json) throw const FormatException('Expected JSON object');
  return value;
}
