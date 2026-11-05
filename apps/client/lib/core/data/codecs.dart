import 'package:omni_folio_client/core/data/json_codec.dart';
import 'package:omni_folio_client/core/domain/models.dart';

Money decodeMoney(Json json) =>
    Money(jsonText(json, 'currency'), jsonDecimal(json, 'amount'));
