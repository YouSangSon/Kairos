import 'package:flutter/widgets.dart';
import 'package:omni_folio_client/app/app.dart';
import 'package:omni_folio_client/app/client_dependencies.dart';
import 'package:omni_folio_client/core/data/http_transport.dart';

void main() => runApp(
  OmniFolioApp(dependencies: ClientDependencies.rest(HttpTransport())),
);
