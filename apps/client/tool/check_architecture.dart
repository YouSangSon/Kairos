import 'dart:io';

const _packagePrefix = 'package:omni_folio_client/';
const _pureDart = {'dart:async', 'dart:collection', 'dart:core', 'dart:math'};

List<String> checkDirectory(Directory directory) => architectureViolations({
  for (final file in directory.listSync(recursive: true).whereType<File>())
    if (file.path.endsWith('.dart'))
      file.absolute.path
          .substring(
            directory.absolute.path.length +
                (directory.path.endsWith('/') ? 0 : 1),
          )
          .replaceAll(Platform.pathSeparator, '/'): file
          .readAsStringSync(),
});

List<String> architectureViolations(Map<String, String> sources) {
  final violations = <String>[];
  final directives = <String, List<_Directive>>{};
  final libraries = <String, String>{};
  for (final entry in sources.entries) {
    if (_boundary(entry.key) == null) {
      violations.add('${entry.key}: source has no architectural owner');
    }
    try {
      final parsed = _directives(entry.value);
      directives[entry.key] = parsed;
      for (final directive in parsed) {
        if (directive.kind == 'library' && directive.value.isNotEmpty) {
          if (libraries.containsKey(directive.value)) {
            violations.add('${entry.key}: duplicate named library');
          }
          libraries[directive.value] = entry.key;
        }
      }
    } on FormatException catch (error) {
      violations.add('${entry.key}: ${error.message}');
    }
  }
  for (final entry in directives.entries) {
    for (final directive in entry.value) {
      if (directive.kind == 'library') continue;
      final uri = directive.value;
      final isPart = directive.kind.startsWith('part');
      if (directive.kind == 'part of name') {
        final target = libraries[uri];
        if (target == null || _boundary(entry.key) != _boundary(target)) {
          violations.add(
            '${entry.key}: part crosses its owner or layer ($uri)',
          );
        }
        continue;
      }
      if (uri.startsWith('dart:') ||
          (uri.startsWith('package:') && !uri.startsWith(_packagePrefix))) {
        final boundary = _boundary(entry.key);
        if (isPart || (_isPure(boundary) && !_pureDart.contains(uri))) {
          violations.add('${entry.key}: forbidden platform dependency $uri');
        }
        continue;
      }
      final target = _resolve(entry.key, uri);
      if (target == null || !sources.containsKey(target)) {
        violations.add('${entry.key}: unresolved source $uri');
      } else if (isPart
          ? _boundary(entry.key) != _boundary(target)
          : !_allowed(entry.key, target)) {
        violations.add(
          '${entry.key}: ${directive.kind} crosses a boundary ($target)',
        );
      }
    }
  }
  return violations;
}

String? _boundary(String path) {
  final parts = path.split('/');
  if (path == 'main.dart') return 'main';
  if (parts.length > 1 && parts.first == 'app') return 'app';
  const layers = {'domain', 'application', 'data', 'presentation'};
  if (parts.length > 2 && parts.first == 'core' && layers.contains(parts[1])) {
    return 'core/${parts[1]}';
  }
  const features = {
    'portfolio',
    'ledger',
    'market_data',
    'connections',
    'watchlist',
    'research',
    'paper',
  };
  if (parts.length > 3 &&
      parts.first == 'features' &&
      features.contains(parts[1]) &&
      layers.contains(parts[2])) {
    return '${parts[1]}/${parts[2]}';
  }
  return null;
}

bool _isPure(String? boundary) =>
    boundary?.endsWith('/domain') == true ||
    boundary?.endsWith('/application') == true;

bool _allowed(String source, String target) {
  final from = _boundary(source), to = _boundary(target);
  if (from == null || to == null) return false;
  if (from == 'app') return true;
  if (from == 'main') return to == 'app' || to.startsWith('core/');
  final owner = from.split('/').first, layer = from.split('/').last;
  final targetOwner = to.split('/').first, targetLayer = to.split('/').last;
  if (to == 'app' || to == 'main') return false;
  if (owner == 'core' && targetOwner != 'core') return false;
  if (targetOwner == 'core' &&
      layer == 'presentation' &&
      !const {'domain', 'presentation'}.contains(targetLayer))
    return false;
  if (layer == 'domain') return targetLayer == 'domain';
  if (layer == 'application') {
    return targetLayer == 'domain' ||
        (owner == targetOwner && layer == targetLayer);
  }
  if (targetLayer == 'domain') return true;
  if (owner != targetOwner && targetOwner != 'core') return false;
  return layer == 'data'
      ? const {'data', 'application'}.contains(targetLayer)
      : const {'presentation', 'application', 'data'}.contains(targetLayer);
}

String? _resolve(String source, String value) {
  try {
    final base = Uri.parse('file:///client/lib/');
    final decoded = Uri.decodeComponent(value);
    final uri = Uri.parse(decoded);
    final target = value.startsWith(_packagePrefix)
        ? base.resolve(decoded.substring(_packagePrefix.length)).normalizePath()
        : base.resolve(source).resolveUri(uri).normalizePath();
    if (target.scheme != 'file' ||
        target.hasQuery ||
        target.hasFragment ||
        !target.path.startsWith(base.path))
      return null;
    return target.path.substring(base.path.length);
  } on FormatException {
    return null;
  }
}

class _Directive {
  const _Directive(this.kind, this.value);
  final String kind, value;
}

class _Token {
  const _Token(this.value, {this.string = false});
  final String value;
  final bool string;
}

List<_Directive> _directives(String source) {
  final tokens = _tokens(source), result = <_Directive>[];
  var depth = 0;
  for (var i = 0; i < tokens.length; i++) {
    final token = tokens[i];
    if (token.string) continue;
    if (token.value == '{') depth++;
    if (token.value == '}') depth--;
    if (depth != 0 ||
        !const {'import', 'export', 'part', 'library'}.contains(token.value)) {
      continue;
    }
    final end = tokens.indexWhere((t) => !t.string && t.value == ';', i + 1);
    if (end < 0) throw const FormatException('Unterminated library directive');
    final values = tokens.sublist(i + 1, end);
    if (values.isEmpty && token.value != 'library') {
      throw const FormatException('Library directive requires a target');
    }
    if (token.value == 'library') {
      result.add(_Directive('library', values.map((t) => t.value).join()));
    } else if (token.value == 'part' && values.first.value == 'of') {
      result.add(
        _Directive(
          values[1].string ? 'part of' : 'part of name',
          values.skip(1).map((t) => t.value).join(),
        ),
      );
    } else {
      if (values.isEmpty || !values.first.string) {
        throw const FormatException('Library directive requires a URI');
      }
      result.add(_Directive(token.value, values.first.value));
      for (var j = 1; j < values.length; j++) {
        if (values[j].string || values[j].value != 'if') continue;
        var conditionDepth = 0;
        do {
          j++;
          if (values[j].value == '(') conditionDepth++;
          if (values[j].value == ')') conditionDepth--;
        } while (conditionDepth > 0 && j + 1 < values.length);
        j++;
        if (j >= values.length || !values[j].string) {
          throw const FormatException('Conditional directive requires a URI');
        }
        result.add(_Directive(token.value, values[j].value));
      }
    }
    i = end;
  }
  return result;
}

List<_Token> _tokens(String source) {
  final result = <_Token>[];
  var i = 0;
  while (i < source.length) {
    if (RegExp(r'\s').hasMatch(source[i])) {
      i++;
      continue;
    }
    if (source.startsWith('//', i)) {
      final end = source.indexOf('\n', i);
      i = end < 0 ? source.length : end + 1;
      continue;
    }
    if (source.startsWith('/*', i)) {
      var depth = 1;
      i += 2;
      while (i < source.length && depth > 0) {
        if (source.startsWith('/*', i)) {
          depth++;
          i += 2;
        } else if (source.startsWith('*/', i)) {
          depth--;
          i += 2;
        } else {
          i++;
        }
      }
      if (depth != 0) throw const FormatException('Unterminated comment');
      continue;
    }
    final raw =
        source[i] == 'r' &&
        i + 1 < source.length &&
        const {"'", '"'}.contains(source[i + 1]);
    if (raw || const {"'", '"'}.contains(source[i])) {
      if (raw) i++;
      final quote = source[i];
      final marker = source.startsWith(quote * 3, i) ? quote * 3 : quote;
      i += marker.length;
      final value = StringBuffer();
      while (i < source.length && !source.startsWith(marker, i)) {
        if (!raw && source[i] == r'\') {
          i++;
          if (i >= source.length) throw const FormatException('Invalid escape');
          final escape = source[i++];
          if (escape == 'u' || escape == 'x') {
            final braced = escape == 'u' && source[i] == '{';
            if (braced) i++;
            final end = braced
                ? source.indexOf('}', i)
                : i + (escape == 'u' ? 4 : 2);
            if (end < i || end > source.length)
              throw const FormatException('Invalid unicode escape');
            value.writeCharCode(int.parse(source.substring(i, end), radix: 16));
            i = end + (braced ? 1 : 0);
          } else {
            value.write(
              const {
                    'n': '\n',
                    'r': '\r',
                    't': '\t',
                    'b': '\b',
                    'f': '\f',
                    'v': '\v',
                  }[escape] ??
                  escape,
            );
          }
        } else {
          value.write(source[i++]);
        }
      }
      if (i >= source.length)
        throw const FormatException('Unterminated string');
      i += marker.length;
      result.add(_Token(value.toString(), string: true));
      continue;
    }
    final identifier = RegExp(
      r'[A-Za-z_$][A-Za-z0-9_$]*',
    ).matchAsPrefix(source, i);
    if (identifier != null) {
      result.add(_Token(identifier[0]!));
      i = identifier.end;
    } else {
      result.add(_Token(source[i++]));
    }
  }
  return result;
}

void main(List<String> arguments) {
  final failures = checkDirectory(
    Directory(arguments.isEmpty ? 'lib' : arguments.single),
  );
  for (final failure in failures) {
    stderr.writeln(failure);
  }
  if (failures.isNotEmpty) exitCode = 1;
}
