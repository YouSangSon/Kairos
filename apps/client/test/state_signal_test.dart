import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:omni_folio_client/core/domain/state_signal.dart';
import 'package:omni_folio_client/core/presentation/signal_builder.dart';

void main() {
  test('notification tolerates listener changes and stops after disposal', () {
    final signal = StateSignal();
    final calls = <String>[];
    void later() => calls.add('later');
    void removed() => calls.add('removed');
    signal.addListener(() {
      calls.add('first');
      signal.removeListener(removed);
      signal.addListener(later);
    });
    signal.addListener(removed);
    signal.notifyListeners();
    expect(calls, ['first']);
    signal.notifyListeners();
    expect(calls, ['first', 'first', 'later']);
    signal.dispose();
    signal.notifyListeners();
    expect(calls, ['first', 'first', 'later']);
  });

  testWidgets(
    'Flutter bridge replaces subscriptions and detaches on disposal',
    (tester) async {
      final first = StateSignal(), second = StateSignal();
      var builds = 0;
      Widget view(StateSignal signal) => Directionality(
        textDirection: TextDirection.ltr,
        child: SignalBuilder(
          signal: signal,
          builder: (_, _) {
            builds++;
            return const Text('observation');
          },
        ),
      );
      await tester.pumpWidget(view(first));
      first.notifyListeners();
      await tester.pump();
      expect(builds, 2);
      await tester.pumpWidget(view(second));
      final replaced = builds;
      first.notifyListeners();
      await tester.pump();
      expect(builds, replaced);
      second.notifyListeners();
      await tester.pump();
      expect(builds, replaced + 1);
      await tester.pumpWidget(const SizedBox());
      second.notifyListeners();
      await tester.pump();
      expect(tester.takeException(), isNull);
      first.dispose();
      second.dispose();
    },
  );
}
