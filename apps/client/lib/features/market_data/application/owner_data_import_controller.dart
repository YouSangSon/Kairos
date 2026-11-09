import 'package:omni_folio_client/core/domain/api_exception.dart';
import 'package:omni_folio_client/core/domain/state_signal.dart';
import 'package:omni_folio_client/features/market_data/domain/market_data_repository.dart';
import 'package:omni_folio_client/features/market_data/domain/models.dart';

class OwnerDataImportController extends StateSignal {
  OwnerDataImportController(
    this.api, {
    required this.onStored,
    required this.pickFile,
  });
  final MarketDataRepository api;
  final void Function() onStored;
  final Future<String?> Function() pickFile;
  String? input;
  OwnerDataPreview? review;
  OwnerDataPreview? pending;
  OwnerDataReceipt? receipt;
  String? message;
  bool selecting = false, reviewing = false, applying = false;
  bool _everUncertain = false, _disposed = false;
  int _generation = 0;

  Future<void> selectFile([Future<String?> Function()? picker]) async {
    if (_disposed || pending != null || applying) return;
    final generation = ++_generation;
    selecting = true;
    reviewing = false;
    _notify();
    try {
      final selected = await (picker ?? pickFile)();
      if (_disposed || generation != _generation || selected == null) return;
      input = selected;
      review = null;
      message = null;
    } catch (_) {
      if (!_disposed && generation == _generation) {
        message = 'UTF-8 JSON 파일을 1MiB 이하로 선택하세요. 기존 입력과 결과는 유지됩니다.';
      }
    } finally {
      if (!_disposed && generation == _generation) {
        selecting = false;
        _notify();
      }
    }
  }

  Future<void> preview() async {
    final original = input;
    if (_disposed ||
        original == null ||
        selecting ||
        reviewing ||
        pending != null) {
      return;
    }
    final generation = ++_generation;
    reviewing = true;
    message = null;
    _notify();
    try {
      final response = await api.previewOwnerData(original);
      if (!_disposed && generation == _generation) {
        if (response.input != original) {
          throw const FormatException('Wrong input review');
        }
        review = response;
      }
    } catch (_) {
      if (!_disposed && generation == _generation) {
        review = null;
        message = '입력 형식을 확인하지 못했습니다. 파일 계약과 서버 연결을 확인하세요.';
      }
    } finally {
      if (!_disposed && generation == _generation) {
        reviewing = false;
        _notify();
      }
    }
  }

  Future<void> apply() async {
    if (_disposed || applying || selecting || reviewing) return;
    pending ??= review;
    final request = pending;
    if (request == null) return;
    applying = true;
    message = null;
    _notify();
    try {
      final result = await api.applyOwnerData(request);
      if (_disposed) return;
      // Validate fake/custom clients too; only a matching receipt resolves uncertainty.
      if (result.review.kind != request.kind ||
          result.review.input != request.input ||
          !result.review.summary.sameAs(request.summary)) {
        throw const FormatException('Wrong stored receipt');
      }
      receipt = result;
      pending = null;
      review = null;
      _everUncertain = false;
      message = '저장 확인서를 받았습니다. 과거 버전 재시도는 최신 버전을 바꾸지 않습니다.';
      onStored();
    } catch (error) {
      if (_disposed) return;
      final rejected =
          error is ApiException &&
          ((error.statusCode == 400 && error.code == 'owner_data_invalid') ||
              (error.statusCode == 409 &&
                  error.code == 'owner_data_conflict') ||
              (error.statusCode == 403 &&
                  error.code == 'owner_data_origin_rejected'));
      if (rejected && !_everUncertain) {
        pending = null;
        review = null;
        message = '저장이 거절되었습니다. 종목·버전·출처 충돌과 서버 허용 주소를 확인하세요.';
      } else {
        _everUncertain = true;
        message = '저장 여부가 불확실합니다. 다른 파일로 바꾸지 말고 같은 원본으로 재시도하세요.';
      }
    } finally {
      if (!_disposed) {
        applying = false;
        _notify();
      }
    }
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}
