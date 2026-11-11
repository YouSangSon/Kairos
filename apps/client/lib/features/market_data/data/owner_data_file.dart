import 'package:file_selector/file_selector.dart';
import 'package:omni_folio_client/core/data/utf8_file.dart';

Future<String?> selectOwnerDataFile() async {
  final file = await openFile(
    acceptedTypeGroups: const [
      XTypeGroup(
        label: 'JSON',
        extensions: ['json'],
        mimeTypes: ['application/json'],
        uniformTypeIdentifiers: ['public.json'],
      ),
    ],
  );
  return file == null ? null : readUtf8File(file);
}
