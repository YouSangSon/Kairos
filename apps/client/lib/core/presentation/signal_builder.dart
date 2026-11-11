import 'package:flutter/widgets.dart';
import 'package:omni_folio_client/core/domain/state_signal.dart';

class SignalBuilder extends StatefulWidget {
  const SignalBuilder({super.key, required this.signal, required this.builder});

  final StateSignal signal;
  final Widget Function(BuildContext, Widget?) builder;

  @override
  State<SignalBuilder> createState() => _SignalBuilderState();
}

class _SignalBuilderState extends State<SignalBuilder> {
  @override
  void initState() {
    super.initState();
    widget.signal.addListener(_changed);
  }

  @override
  void didUpdateWidget(SignalBuilder oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.signal != widget.signal) {
      oldWidget.signal.removeListener(_changed);
      widget.signal.addListener(_changed);
    }
  }

  void _changed() => setState(() {});

  @override
  void dispose() {
    widget.signal.removeListener(_changed);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.builder(context, null);
}
