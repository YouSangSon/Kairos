import 'package:flutter/material.dart';
import 'package:omni_folio_client/core/domain/models.dart';

class SectionCard extends StatelessWidget {
  const SectionCard({super.key, required this.title, required this.child});
  final String title;
  final Widget child;
  @override
  Widget build(BuildContext context) => Card(
    child: Padding(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Semantics(
            container: true,
            header: true,
            child: Text(title, style: Theme.of(context).textTheme.titleMedium),
          ),
          const SizedBox(height: 12),
          child,
        ],
      ),
    ),
  );
}

class Notice extends StatelessWidget {
  const Notice({
    super.key,
    required this.icon,
    required this.text,
    required this.color,
  });
  final IconData icon;
  final String text;
  final Color color;
  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.all(16),
    decoration: BoxDecoration(
      border: Border.all(color: color),
      borderRadius: BorderRadius.circular(16),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, color: color),
        const SizedBox(width: 12),
        Expanded(child: Text(text)),
      ],
    ),
  );
}

class Message extends StatelessWidget {
  const Message({
    super.key,
    required this.icon,
    required this.title,
    required this.body,
    this.action,
    this.actionLabel,
  });
  final IconData icon;
  final String title;
  final String body;
  final VoidCallback? action;
  final String? actionLabel;
  @override
  Widget build(BuildContext context) => Center(
    child: SingleChildScrollView(
      padding: const EdgeInsets.all(24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 72,
            height: 72,
            decoration: BoxDecoration(
              color: Theme.of(
                context,
              ).colorScheme.primary.withValues(alpha: 0.1),
              shape: BoxShape.circle,
            ),
            child: Icon(
              icon,
              size: 34,
              color: Theme.of(context).colorScheme.primary,
            ),
          ),
          const SizedBox(height: 16),
          Text(
            title,
            style: Theme.of(context).textTheme.titleMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 8),
          Text(body, textAlign: TextAlign.center),
          if (action != null) ...[
            const SizedBox(height: 16),
            SizedBox(
              key: const Key('empty-primary-action'),
              width: 320,
              child: ElevatedButton(
                onPressed: action,
                child: Text(actionLabel!),
              ),
            ),
          ],
        ],
      ),
    ),
  );
}

class Loading extends StatelessWidget {
  const Loading({super.key, this.compact = false});
  final bool compact;
  @override
  Widget build(BuildContext context) => Semantics(
    label: '데이터를 불러오는 중',
    liveRegion: true,
    child: Center(
      child: SizedBox(
        height: compact ? 24 : 48,
        width: compact ? 24 : 48,
        child: MediaQuery.disableAnimationsOf(context)
            ? const Icon(Icons.hourglass_empty)
            : const CircularProgressIndicator(),
      ),
    ),
  );
}

TextStyle tabular(BuildContext context) => Theme.of(context)
    .textTheme
    .bodyLarge!
    .copyWith(fontFeatures: const [FontFeature.tabularFigures()]);

Color positiveColor(BuildContext context) =>
    Theme.of(context).brightness == Brightness.dark
    ? const Color(0xFF34D399)
    : const Color(0xFF047857);

Color warningColor(BuildContext context) =>
    Theme.of(context).brightness == Brightness.dark
    ? const Color(0xFFFCD34D)
    : const Color(0xFFA16207);

String moneySummary(List<Money> money) => money.isEmpty
    ? '없음'
    : money.map((item) => '${item.currency} ${item.amount}').join(' · ');

String formattedMoney(Money money) {
  final negative = money.amount.startsWith('-');
  final unsigned = negative ? money.amount.substring(1) : money.amount;
  final parts = unsigned.split('.');
  final whole = parts.first;
  final buffer = StringBuffer();
  for (var index = 0; index < whole.length; index += 1) {
    if (index > 0 && (whole.length - index) % 3 == 0) buffer.write(',');
    buffer.write(whole[index]);
  }
  final fraction = parts.length == 1 ? '' : '.${parts[1]}';
  return '${money.currency} ${negative ? '-' : ''}$buffer$fraction';
}

bool isZero(String amount) => RegExp(r'^-?0(?:\.0+)?$').hasMatch(amount);

String time(DateTime value) => value.toLocal().toString().substring(0, 16);
