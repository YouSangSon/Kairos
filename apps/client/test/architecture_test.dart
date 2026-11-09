import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

import '../tool/check_architecture.dart';

void main() {
  test('all production sources obey the feature boundaries', () {
    expect(checkDirectory(Directory('lib')), isEmpty);
  });

  test('normalizes relative and package imports through nested folders', () {
    for (final uri in [
      '../../../../watchlist/data/prices.dart',
      'package:omni_folio_client/features/watchlist/data/prices.dart',
      'package:omni_folio_client/features/portfolio/domain/../../watchlist/data/prices.dart',
      'package:omni_folio_client/features/portfolio/domain/%2e%2e/%2e%2e/watchlist/data/prices.dart',
    ]) {
      final failures = architectureViolations({
        'features/portfolio/domain/deep/nested/value.dart': "import '$uri';",
        'features/watchlist/data/prices.dart': '',
      });
      expect(failures, isNotEmpty, reason: uri);
    }
  });

  test(
    'checks export and every conditional URI, including escaped strings',
    () {
      for (final source in [
        "export 'package:http/http.dart';",
        "import 'value.dart' if (dart.library.io) 'dart:io';",
        r"import 'value.dart' if (dart.library.io == 'true') '\u0064art:io';",
        "export r'package:flutter/widgets.dart';",
      ]) {
        expect(
          architectureViolations({
            'features/ledger/domain/model.dart': source,
            'features/ledger/domain/value.dart': '',
          }),
          isNotEmpty,
          reason: source,
        );
      }
    },
  );

  test('comments and ordinary strings cannot create fake dependencies', () {
    expect(
      architectureViolations({
        'features/ledger/domain/value.dart': '''
/* import 'dart:io'; /* nested */ */
// export 'package:flutter/widgets.dart';
const text = "import 'dart:io';";
''',
      }),
      isEmpty,
    );
  });

  test('ignores nested interpolation after the library directives', () {
    expect(
      architectureViolations({
        'features/ledger/data/csv.dart': r'''
import '../domain/value.dart';
String escape(String value) => '"${value.replaceAll('"', '""')}"';
''',
        'features/ledger/domain/value.dart': '',
      }),
      isEmpty,
    );
  });

  test('metadata cannot hide directives or require lexing declarations', () {
    expect(
      architectureViolations({
        'features/ledger/domain/value.dart': r'''
@Deprecated('old API')
library ledger;
@deprecated
import 'dart:math';
@Deprecated('old API')
class Value {
  String escape(String value) => '"${value.replaceAll('"', '""')}"';
}
''',
      }),
      isEmpty,
    );
    expect(
      architectureViolations({
        'features/ledger/domain/value.dart':
            "@Deprecated('old API') import 'package:http/http.dart';",
      }),
      contains(contains('forbidden platform dependency')),
    );
  });

  test('malformed directives report violations without aborting the gate', () {
    for (final source in ["import 'value.dart' if;", 'part of;']) {
      expect(
        architectureViolations({'features/ledger/domain/value.dart': source}),
        isNotEmpty,
      );
    }
  });

  test('interpolated metadata cannot hide imports or exports', () {
    for (final directive in ["import 'dart:io';", "export 'dart:io';"]) {
      expect(
        architectureViolations({
          'features/ledger/domain/value.dart':
              r"@Deprecated('${')'}') " + directive,
        }),
        isNotEmpty,
        reason: directive,
      );
    }
  });

  test('rejects import, export and conditional dependency cycles', () {
    for (final directive in [
      "import 'nested/b.dart';",
      "export 'nested/b.dart';",
      "import 'value.dart' if (dart.library.io) 'nested/b.dart';",
    ]) {
      expect(
        architectureViolations({
          'features/ledger/domain/a.dart': directive,
          'features/ledger/domain/nested/b.dart': "import '../a.dart';",
          'features/ledger/domain/value.dart': '',
        }).any((failure) => failure.contains('dependency cycle')),
        isTrue,
        reason: directive,
      );
    }
  });

  test('rejects feature cycles even when individual files are acyclic', () {
    expect(
      architectureViolations({
        'features/ledger/domain/a.dart':
            "import '../../portfolio/domain/a.dart';",
        'features/portfolio/domain/a.dart': '',
        'features/portfolio/domain/b.dart':
            "import '../../ledger/domain/b.dart';",
        'features/ledger/domain/b.dart': '',
      }).any((failure) => failure.contains('dependency cycle')),
      isTrue,
    );
  });

  test('part cycles are rejected while part-of ownership is not a cycle', () {
    expect(
      architectureViolations({
        'features/ledger/domain/a.dart': "part 'b.dart';",
        'features/ledger/domain/b.dart': "part 'a.dart';",
      }),
      contains(contains('dependency cycle')),
    );
  });

  test('part and named part-of stay inside the same owner and layer', () {
    expect(
      architectureViolations({
        'features/ledger/domain/value.dart':
            "library ledger; part 'nested/value_part.dart';",
        'features/ledger/domain/nested/value_part.dart': 'part of ledger;',
      }),
      isEmpty,
    );
    for (final source in [
      "part '../../watchlist/data/value.dart';",
      "part of '../../watchlist/data/value.dart';",
      'part of adapter;',
    ]) {
      expect(
        architectureViolations({
          'features/ledger/domain/value.dart': source,
          'features/watchlist/data/value.dart': 'library adapter;',
        }),
        isNotEmpty,
        reason: source,
      );
    }
  });

  test('composition can connect features while core stays independent', () {
    expect(
      architectureViolations({
        'app/composition.dart': "import '../features/ledger/data/rest.dart';",
        'features/ledger/data/rest.dart': "import '../domain/value.dart';",
        'features/ledger/domain/value.dart':
            "import '../../../core/domain/money.dart';",
        'core/domain/money.dart': '',
      }),
      isEmpty,
    );
    expect(
      architectureViolations({
        'core/domain/money.dart':
            "import '../../features/ledger/domain/value.dart';",
        'features/ledger/domain/value.dart': '',
      }),
      isNotEmpty,
    );
  });

  test(
    'recursive discovery includes new nested sources and unknown owners',
    () {
      final temporary = Directory.systemTemp.createTempSync(
        'kairos-client-boundaries-',
      );
      addTearDown(() => temporary.deleteSync(recursive: true));
      final directory = Directory('${temporary.path}/lib')..createSync();
      File('${directory.path}/features/paper/application/new/nested.dart')
        ..createSync(recursive: true)
        ..writeAsStringSync("import 'dart:io';");
      File('${directory.path}/unowned/new.dart')
        ..createSync(recursive: true)
        ..writeAsStringSync('');
      final failures = checkDirectory(directory);
      expect(failures, hasLength(2));
      expect(
        failures.any((failure) => failure.contains('new/nested.dart')),
        isTrue,
      );
      expect(
        failures.any((failure) => failure.contains('no architectural owner')),
        isTrue,
      );
    },
  );
}
