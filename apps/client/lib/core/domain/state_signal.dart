typedef StateListener = void Function();

class StateSignal {
  final _listeners = <StateListener>[];
  bool _closed = false;

  void addListener(StateListener listener) {
    if (!_closed) _listeners.add(listener);
  }

  void removeListener(StateListener listener) => _listeners.remove(listener);

  void notifyListeners() {
    if (_closed) return;
    for (final listener in List<StateListener>.of(_listeners)) {
      if (_listeners.contains(listener)) listener();
    }
  }

  void dispose() {
    _closed = true;
    _listeners.clear();
  }
}
