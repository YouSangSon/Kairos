import 'dart:convert';
import 'dart:typed_data';
import 'package:file_selector/file_selector.dart';

const maxUtf8FileBytes = 1 << 20;

Future<String> readUtf8File(XFile file) async {
  if (await file.length() > maxUtf8FileBytes) {
    throw RangeError('File exceeds 1MiB');
  }
  final bytes = BytesBuilder(copy: false);
  // Do not use metadata as the endpoint: a file may grow after length().
  await for (final chunk in file.openRead()) {
    if (bytes.length + chunk.length > maxUtf8FileBytes) {
      throw RangeError('File exceeds 1MiB');
    }
    bytes.add(chunk);
  }
  return utf8.decode(bytes.takeBytes());
}
