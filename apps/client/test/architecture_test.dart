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
