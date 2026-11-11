import 'dart:convert';
import 'package:omni_folio_client/core/domain/decimal.dart';
import 'package:omni_folio_client/core/domain/models.dart';

typedef Json = Map<String, dynamic>;

void jsonRequireExactKeys(Json value, Set<String> keys) {
  if (value.length != keys.length || !value.keys.every(keys.contains)) {
    throw const FormatException('Unexpected object fields');
  }
}

String jsonText(Json value, String key) {
  final result = value[key];
  if (result is! String || result.isEmpty) {
    throw const FormatException('Missing text field');
  }
  return result;
}

String jsonDecimal(Json value, String key) {
  final result = jsonText(value, key);
  if (!canonicalDecimalPattern.hasMatch(result)) {
    throw const FormatException('Decimal fields must be canonical strings');
  }
  return result;
}

int jsonCount(Json value, String key) {
  final result = value[key];
  if (result is! int || result < 0) {
    throw const FormatException('Invalid count');
  }
  return result;
}

bool jsonBool(Json value, String key) {
  final result = value[key];
  if (result is! bool) {
    throw const FormatException('Missing bool field');
  }
  return result;
}

DateTime? jsonOptionalDate(Json value, String key) {
  final result = value[key];
  if (result == null) return null;
  return jsonRfc3339(result);
}

DateTime jsonRfc3339(Object? value) {
  if (value is! String ||
      !RegExp(
        r'^\d{4}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12]\d|3[01])T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d+)?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$',
      ).hasMatch(value)) {
    throw const FormatException('Date must be RFC3339 text');
  }
  try {
    return DateTime.parse(value);
  } on FormatException {
    throw const FormatException('Invalid RFC3339 date');
  }
}

String jsonRfc3339Text(Object? value) {
  jsonRfc3339(value);
  return value! as String;
}

String? jsonOptionalRfc3339Text(Json value, String key) {
  final result = value[key];
  return result == null ? null : jsonRfc3339Text(result);
}

List<Json> jsonJsonList(Json value, String key) {
  final result = value[key];
  if (result is! List<dynamic>) {
    throw const FormatException('Missing list field');
  }
  return result
      .map((item) {
        if (item is! Json) {
          throw const FormatException('List item must be an object');
        }
        return item;
      })
      .toList(growable: false);
}

List<Json> jsonOptionalJsonList(Json value, String key) {
  final result = value[key];
  if (result == null) {
    return const [];
  }
  if (result is! List<dynamic>) {
    throw const FormatException('List field must be an array');
  }
  return result
      .map((item) {
        if (item is! Json) {
          throw const FormatException('List item must be an object');
        }
        return item;
      })
      .toList(growable: false);
}

List<Money> jsonMoneyList(Json value, String key) =>
    jsonJsonList(value, key).map(decodeMoney).toList(growable: false);

int jsonNonEmptyStringCount(Json value, String key) {
  final result = value[key];
  if (result is! List<dynamic> ||
      result.any((item) => item is! String || item.isEmpty)) {
    throw const FormatException('Invalid provenance IDs');
  }
  return result.length;
}

String jsonCurrency(Json value, String key) {
  final result = jsonText(value, key);
  if (!RegExp(r'^[A-Z]{3}$').hasMatch(result)) {
    throw const FormatException('Invalid currency');
  }
  return result;
}

String jsonUtcText(Json value, String key) {
  final result = jsonRfc3339Text(value[key]);
  if (!RegExp(
    r'^\d{4}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12]\d|3[01])T(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d{0,8}[1-9])?Z$',
  ).hasMatch(result)) {
    throw const FormatException('Timestamp must be canonical UTC');
  }
  final parsed = DateTime.parse(result);
  final match = RegExp(
    r'^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})',
  ).firstMatch(result)!;
  final parts = [
    parsed.year,
    parsed.month,
    parsed.day,
    parsed.hour,
    parsed.minute,
    parsed.second,
  ];
  for (var index = 0; index < parts.length; index += 1) {
    if (parts[index] != int.parse(match.group(index + 1)!)) {
      throw const FormatException('Invalid UTC timestamp');
    }
  }
  return result;
}

String? jsonNullableText(Json value, String key) {
  final result = value[key];
  if (result == null) return null;
  if (result is! String || result.isEmpty) {
    throw const FormatException('Invalid nullable text');
  }
  return result;
}

String? jsonNullableDecimal(Json value, String key) {
  if (value[key] == null) return null;
  return jsonDecimal(value, key);
}

String jsonBenchmarkText(
  Json json,
  String key,
  int maxBytes, {
  bool identity = false,
  bool trimmed = false,
}) {
  final text = jsonText(json, key);
  final controls = RegExp(r'[\u0000-\u001f\u007f-\u009f]');
  final spaces = RegExp(
    r'[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]',
  );
  if (text.runes.any((rune) => rune >= 0xd800 && rune <= 0xdfff) ||
      utf8.encode(text).length > maxBytes ||
      controls.hasMatch(text) ||
      (identity && spaces.hasMatch(text)) ||
      (trimmed &&
          (spaces.hasMatch(text.substring(0, 1)) ||
              spaces.hasMatch(text.substring(text.length - 1))))) {
    throw const FormatException('Invalid benchmark text');
  }
  return text;
}

Money decodeMoney(Json json) =>
    Money(jsonText(json, 'currency'), jsonDecimal(json, 'amount'));

Json decodeObject(String body) {
  final value = jsonDecode(body);
  if (value is! Json) throw const FormatException('Expected JSON object');
  return value;
}
