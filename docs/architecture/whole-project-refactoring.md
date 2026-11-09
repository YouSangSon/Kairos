# Kairos 전체 리팩토링 설계

- 설계 기준일: 2026-11-05. 코드 기준: `add8787eaa5f348daeea97df2df5cd06de3b90a8`.
- 범위: Go core, Flutter client, Python research, 계약·테스트·빌드·운영 도구의 연결부.
- 상태: 구현은 중단 상태이며, 2026-11-09 요청으로 15절의 모듈·소유권 보완 설계를 작성했다. 아래 경로·인터페이스는 목표 구조다. 실제 적용 범위는 14절, 후속 설계 제안은 15절을 따른다.
- 상위 결정: [ADR-0002](../adr/0002-domain-module-boundaries.md). 제품 기능과 승격 권한은 [CONTEXT](../../CONTEXT.md), [GATES](../../GATES.md)가 소유한다.

## 1. 목표와 설계의 한계

Kairos는 정보 수집, 투자 기회 탐지, 추천, 허용된 실행, 리밸런싱, 성과 분석과 복기를 연결하는 제품이다. 이번 변경은 이 기능을 확장할 때 기존 회계·주문·복구 규칙을 깨뜨리지 않도록 책임을 분리한다. 새로운 자동 투자 권한이나 수익 보장을 추가하지 않는다. 현재 `live_enabled=false`와 각 gate의 제한을 그대로 보존한다.

DDD는 업무별 용어와 규칙의 소유자를 정하는 방법으로 적용한다. 클린 아키텍처는 그 규칙이 HTTP, SQLite, Flutter 또는 브로커 구현에 의존하지 않도록 적용한다. 모듈화의 완료 기준은 디렉터리 수가 아니라 변경 영향과 금지 의존성을 테스트로 확인할 수 있는가이다.

백엔드는 Go, 클라이언트는 Flutter, 연구는 Python으로 유지한다. asdf의 프로젝트 버전 선택과 lockfile을 유지한다. Rust 전환, 마이크로서비스화, 새 DB·메시지 브로커·상태 관리 라이브러리를 이번 refactor와 함께 설치하지 않는다. MSA·복수 DB·인증·캐시·Kafka/RabbitMQ 단독 및 병행 운영의 목표와 전환 계약은 [플랫폼 확장 설계](scalability-and-platform.md)가 소유한다. Mimir 유래 공개 정보는 연구 입력이며 주문 권한의 근거로 승격되지 않는다.

## 2. 현재 구조와 후보 비교

현재 core의 `Service`는 저장소, 사용 사례, 외부 호출과 HTTP 구성을 함께 소유한다. 순수 계산은 여섯 `internal` 패키지로 일부 분리돼 있으나 대부분의 조합은 `package main`에 남아 있다. Flutter의 `app.dart`, `api.dart`, `models.dart`는 화면·요청·파싱 변경을 넓게 전파한다. Python은 CLI별 파일이 계산과 파일 I/O를 함께 수행한다.

기존 통합 구조에는 보존할 이유도 있다. SQLite 단일 연결과 함께 여러 업무의 기록을 원자적으로 반영하고, Flutter의 앱 수명 객체가 화면 전환 중 쓰기 결과를 보존하며, Python의 공통 계산이 여러 CLI의 재현성을 유지한다. 이 결합을 단순히 잘라내면 데이터 정합성을 잃는다.

| 영역 | 채택한 구조 | 비교한 다른 구조 | 선택 이유와 비용 |
| --- | --- | --- | --- |
| Go | 업무별 application + 하나의 SQLite 어댑터 + 실행 coordinator | 업무마다 독립 저장소·트랜잭션을 가진 수직 모듈 | 기존 교차 업무 원자성을 유지한다. SQL 구현은 한 곳에 모이지만 파일별 상태 소유권과 좁은 포트를 강제해야 한다. |
| Flutter | 한 패키지 안의 7개 feature | feature별 Dart 패키지 | 화면과 데이터 경계를 분리하면서 배포·의존성 관리 비용을 늘리지 않는다. 패키지 내부 접근 제한은 CI 검사로 보완한다. |
| Python | 하나의 research 경계 안에서 계산·사용 사례·I/O 분리 | CLI별 독립 context | 동일 계산과 산출물 계약의 중복을 막는다. CLI 호환 진입점은 얇게 유지해야 한다. |

합성 결정: 기존 탐색에서 확인한 수명·트랜잭션 단위를 보존하는 첫 번째 구조를 기준으로 삼는다. 다른 후보의 명시적인 상태 소유권은 가져오되, 독립 DB와 다중 Dart 패키지는 가져오지 않는다. 범용 repository, 모든 타입의 interface화, `main`을 그대로 옮긴 `internal/core`는 문제를 숨기므로 제외한다.

## 3. 책임과 상태 소유권

| 책임 경계 | 소유하는 결정·상태 | 다른 경계에 제공하는 것 |
| --- | --- | --- |
| ledger | 거래 사실, 정정, preview/apply, revision, FIFO 재생 | 검증된 거래·잔고·revision 읽기 결과 |
| portfolio | 평가 가능성, 현금·보유 평가, XIRR/TWR와 비교 | 원장·가격 출처와 부족 사유가 포함된 읽기 모델 |
| marketdata | 가격·환율·상장·관찰 출처와 시점, watchlist CAS | 관찰 사실과 freshness; 주문 결정은 제공하지 않음 |
| execution | 주문 상태, authority, fence, reservation, 중복 dispatch, reconciliation | 제출 결과와 복구 가능한 상태 |
| research/paper | 전략 증거·선택·모의 회계·성과·승격·runner 정책 | 검증된 후보와 모의 성과; 실행 권한과는 별개 |
| maintenance | schema migration, backup/restore, integrity proof | 검증된 백업과 복구 결과; 투자 정책은 소유하지 않음 |

`execution/coordinator`는 주문·위험·전략·모의 운용의 기존 원자적 사용 사례만 조합한다. 정책 자체는 각 순수 도메인이 소유한다. portfolio가 원장을 쓰거나 research가 브로커를 호출하지 않는다. 읽기 SQL의 join은 명시적인 조회 어댑터에 허용하지만 쓰기 소유권을 넘겨주지 않는다.

## 4. 호출자가 보는 사용 방식

아래 코드는 계약을 설명하는 스케치다. 식별자는 제안이며 기존 wire 타입·오류·필드와 대조해 옮긴다. `not implemented` 코드를 운영 소스에 추가하지 않는다.

```go
// HTTP는 JSON을 해석하고 사용 사례 한 번을 호출한다.
receipt, err := ledgerUseCases.Apply(ctx, command)
// CLI는 transaction/authority 메서드를 조립하지 않는다.
state, err := executionUseCases.SubmitSyntheticLimit(ctx, orderID, fencingToken)
// 읽기 모델은 계산에 사용한 revision과 관찰 출처를 함께 반환한다.
valuation, err := portfolioQueries.CurrentValuation(ctx, query)
```

```dart
// app이 실제 저장소와 session 수명의 controller를 조립한다.
final importSession = ImportApplyController(ledgerRepository);
// 페이지는 captured preview/key를 직접 소유하지 않는다.
await importSession.apply(preview, csv); // controller가 첫 입력과 key를 캡처
await importSession.retry(); // 결과 불명 시 같은 captured 요청 사용
```

```python
# 기존 외부 호출 경로는 유지한다. 내부 구현은 이 facade를 import하지 않는다.
from omni_research.engine import build_manifest
manifest = build_manifest(
    bars_path, request, run_id=run_id, dataset_id=dataset_id,
    started_at=started_at, finished_at=finished_at,
)
```

호출자는 SQL 순서, broker credential, HTTP client 또는 파일 안전 처리를 알 필요가 없다. 반대로 업무 명령, 명시적인 실패 결과, 취소와 관찰 상태는 숨기지 않는다. 같은 인자를 그대로 전달하기만 하는 service/repository 계층은 만들지 않는다.

## 5. Go 목표 구조

```text
services/core/
  main.go                         CLI 진입과 종료 처리
  internal/bootstrap/             구성, 주입, service 수명과 close
  internal/application/
    ledger/ portfolio/ marketdata/ research/ maintenance/
    execution/                    주문 사용 사례와 consumer ports
      coordinator/                교차 업무 원자적 흐름
  internal/adapters/
    sqlite/                       SQL, migration, transaction scopes
    httpapi/ cli/ kiwoom/ files/   각 신뢰 경계의 입출력
  internal/exact/                 기존 정밀 계산
  internal/orderdomain/ paperdomain/ riskdomain/
  internal/researchdomain/ strategydomain/
  internal/ledgerdomain/ portfoliodomain/ marketdomain/
  internal/architecture/          재귀 의존성 검사
  migrations/                    기존 embed·버전·내용 유지
```

새 domain 패키지는 실제 순수 규칙을 이동할 때만 만든다. 기존 여섯 패키지의 이름과 승인된 의존성은 유지한다. root build와 `go run .`는 유지하고, `main`에는 업무 규칙이나 SQL이 남지 않는다.

### 의존성 규칙

- domain → 명시적으로 허용된 순수 표준 라이브러리와 기존 순수 domain. SQL·파일·네트워크·환경·벽시계 접근 금지.
- application → domain과 자신이 소비하는 포트. adapter, HTTP request, SQL row/transaction 타입 금지.
- adapter → application/domain. bootstrap만 구체 구현들을 함께 연결한다.
- feature 간 application 상호 호출로 순환을 만들지 않는다. 공통 작업은 coordinator가 한 scope 안에서 조합한다.
- 공유 타입은 실제 두 소비자가 필요로 하는 값만 둔다. `common`, `utils`, `BaseRepository`를 새 저장소로 만들지 않는다.
- HTTP DTO는 httpapi, DB row는 sqlite, broker payload는 kiwoom이 소유한다. 해시 대상 이벤트의 직렬화는 별도의 안정된 codec 한 곳이 소유하며 재정렬하지 않는다.

### 트랜잭션 포트

consumer가 필요한 인터페이스를 소유하고 sqlite가 구현한다. `Write`는 한 callback 전체를 같은 `*sql.Tx`에 묶고 commit 실패까지 호출자에게 반환한다. `Read`는 기존 snapshot 필요 범위를 보존한다. 모든 조회를 새 transaction으로 바꾸는 의미 변경은 하지 않는다.

```go
// execution/coordinator 소유. 각 journal은 아래 표의 구체 연산만 노출한다.
type UnitOfWork interface {
    Write(context.Context, func(WriteScope) error) error
    Read(context.Context, func(ReadScope) error) error
}
type WriteScope interface {
    Orders() OrderJournal
    Authority() AuthorityJournal
    Reservations() ReservationJournal
    Selections() SelectionJournal
    Paper() PaperJournal
    Promotions() PromotionJournal
    Runner() RunnerLeaseJournal
    Policy() PolicyJournal
    Recovery() RecoveryReader
}
```

| 포트 | 필요한 연산의 형태 | 정책 소유자 |
| --- | --- | --- |
| OrderJournal | `LoadIntent`, `ReadEvents`, `RecordIntent`, `AppendAuthorizedPair`, `AppendOutcome` | orderdomain 전이 + execution 사용 사례 |
| AuthorityJournal | 현재 lease/fence 조회, acquire/arm/갱신/halt 기록 | execution authority |
| ReservationJournal | 활성 예약·한도 입력 읽기, 검증된 예약 기록 | riskdomain + coordinator |
| SelectionJournal | 선택 revision 읽기, 예상 revision을 조건으로 선택·rollback | strategydomain + research |
| PaperJournal | 회계 session 생성/조회, signal 기록·중복 조회, paper authorization 기록, bar capacity 읽기, 전용 capitalized fill 기록 | paperdomain + coordinator |
| PromotionJournal | claim·decision 조회, claim 생성, stale snapshot을 조건으로 decision 기록·abort | research promotion + coordinator |
| RunnerLeaseJournal | owner/fence 조회와 조건부 갱신 | runner 사용 사례 |
| PolicyJournal | 정책 입력·결정 읽기와 결정 기록 | paper 정책 사용 사례 |
| RecoveryReader | 순서가 보존된 이벤트·증거 record 목록 반환 | 증명 조합·허용 판단은 domain/application |

포트 표는 다음 사용 사례별 쓰기 집합을 모두 수용해야 한다. admission은 `Paper.RecordSignal` → `Orders.RecordIntent` → `Paper.RecordAuthorization` → `Orders.AppendAuthorizedPair` → `Orders.AppendOutcome(local ACK)`를 같은 scope로 수행한다. 중복 signal이면 기존 order/bindings를 확인해 기존 결과를 반환하고 새 authorization을 만들지 않는다. fill은 이 admission과 별도 transaction에서 전용 회계 기록을 쓴다. promotion claim/finish, strategy register/select/rollback, paper accounting session 생성/조회, runner claim/renew/release의 각 기존 command도 해당 owner의 명명된 포트로 옮긴다. 새 범용 append로 이 동작을 우회하지 않는다.

`ReadScope`는 위 journal의 읽기 메서드만 제공하는 별도 인터페이스 집합이다. `WriteScope`를 그대로 반환하고 관례로 쓰지 말라고 하지 않는다. 기존 command input/output의 필드·오류 계약은 이동 전에 source와 함께 고정하며, 이 표에 없는 새 업무 동작은 refactor로 추가하지 않는다.

각 연산은 기존 구조체에 대응하는 명명된 command/result와 `error`를 사용한다. `map[string]any`, SQL 문자열, 임의 predicate, `Query/Exec`를 포트로 노출하지 않는다. insert/update의 영향 행 수와 unique 충돌은 어댑터가 명시적인 결과로 변환한다. 금융 판단을 SQL 어댑터 안의 거대한 `RecoverAndApprove`로 숨기지 않는다.

ledger·marketdata는 이 큰 scope를 받지 않는다. 예를 들어 ledger write scope는 preview/receipt/event/revision만, watchlist write scope는 현재 revision과 조건부 교체만 제공한다. portfolio는 읽기 입력만 받는다. scope 구현은 같은 transaction을 공유하며 DB 연결을 다시 획득하지 않는다.

callback 밖으로 scope·iterator·row를 반환하지 않는다. 중첩 `Write`와 callback 안의 DB 기반 service 호출을 금지한다. 단일 연결에서 교착될 수 있기 때문이다. callback 안에서 만든 결과는 commit 성공 후에만 외부로 반환한다. panic/error/cancel은 rollback하고, rollback 실패가 최초 원인을 덮지 않게 한다. 외부 네트워크 호출은 transaction 밖에서 수행한다.

### 분리하면 안 되는 처리 단위

| 흐름과 현재 근거 | 유지할 순서·원자성 | 실패 시 계약 |
| --- | --- | --- |
| `kiwoom_order_transport.go`, `execution_authority.go` | capability/provider/mode 확인 → preflight → 한 transaction에서 fence·증거·미해결 주문·한도·예약·연속 RISK_APPROVED/SUBMIT_DISPATCHED 기록 → commit → broker → outcome transaction | dispatch 후 timeout/crash/ACK 기록 실패는 재제출하지 않고 reconciliation. reject와 unknown 구분 유지 |
| `paper_runner.go` admission | signal·intent·authorization·local ACK를 한 transaction으로 반영 | 일부 승인이나 예약만 남기지 않음 |
| `paper_runner.go` fill | admission과 별도 기존 transaction. proof·계정·bar capacity 확인 → 순수 fill 계산 → 전용 capitalized writer → replay 정확히 1 증가 | 일반 append는 capitalized fill 거부; 계산/기록 실패는 전체 rollback |
| `paper_performance_policy.go` | 정책 결정·관련 armed authority halt·선택 rollback·runner renewal을 함께 commit | 일부 halt만 반영하지 않음 |
| `paper_local_heartbeat.go` | authority와 runner token 갱신을 함께 commit | commit 전 lease 반환 금지 |
| `paper_promotion.go` | claim commit → transaction 밖 점수 계산 → stale-state 재확인 후 finish commit | 취소 시 기존 10초 bounded WithoutCancel finish; crash는 PENDING 유지 |

특히 “사용 사례 하나 = transaction 하나”를 공통 규칙으로 만들지 않는다. promotion은 durable claim과 계산·최종 판정을 나누어야 기존 복구 의미가 유지된다. `now`, ID 생성 위치, in-transaction fence 검사, service 수명의 execution/runner owner와 activity cursor key를 바꾸지 않는다.

### 브로커 capability의 패키지 경계

현재 `LimitOrderDispatchPort`는 임의 broker interface가 아니라 provider/mode와 두 동작이 묶인 capability다. 분리 후에도 application에서 임의 문자열과 함수로 생성하는 public constructor를 제공하지 않는다.

구체 방식: kiwoom adapter가 비공개 필드의 concrete `MockDispatch`와 유일한 `NewMockDispatch(*KiwoomClient)`를 소유한다. factory는 nil과 mock environment를 검증하고 provider/mode를 고정한다. application은 아래 consumer port를 받는다. bootstrap만 production 생성과 등록을 수행한다. application이 adapter의 concrete descriptor를 import하는 순환은 만들지 않는다.

```go
// application/execution 소유. OrderIntent는 이전한 동일 업무 record다.
type LimitOrderDispatch interface {
    Provider() string
    Mode() string
    Preflight(context.Context) error
    SubmitLimitOrder(context.Context, OrderIntent) (string, error)
}
// adapters/kiwoom 소유. fields 비공개, 외부 callback 주입 생성자 없음.
// func NewMockDispatch(client *KiwoomClient) (*MockDispatch, error)
// bootstrap: adapter 생성 → execution use case에 port로 전달
```

이 interface 자체는 Go 타입 체계에서 sealed가 아니다. 봉인은 **adapter의 검증된 생성자와 composition/import 검사**로 유지한다. production 구현은 kiwoom MockDispatch만 허용하고 생성/등록 caller는 bootstrap만 허용한다. application은 nil·provider=`kiwoom`·mode=`synthetic`·intent 일치를 credential preflight 전에 확인한다. adapter도 호출마다 환경/intent를 검증한다. 테스트 fake는 test 파일에만 존재한다. 기존 잘못 연결된 adapter 거부 테스트와 live environment 거부 테스트를 보존한다. 임의 문자열/함수로 capability를 만드는 application public constructor는 없다.

의존 방향은 bootstrap→kiwoom/application, kiwoom→application의 port·domain record, application→domain이다. execution의 coordinator는 execution의 command/port 타입을 사용할 수 있지만 execution package가 coordinator를 다시 import하지 않는다. bootstrap이 coordinator를 만들고 HTTP/CLI의 consumer interface에 연결한다. 다른 provider/live 구현은 별도 기능·권한 gate를 통과하기 전 등록하지 않는다.

### 오류·취소·자원 수명

기존 HTTP status/error body, CLI exit code/stderr, domain refusal, unknown outcome을 그대로 매핑한다. infrastructure 오류에는 원인을 보존하되 credential·계정 원문을 응답이나 로그로 노출하지 않는다. 쓰기 timeout을 자동 재시도로 바꾸지 않는다. context는 요청에서 전달하며 background context로 무조건 대체하지 않는다. 예외는 기존 promotion의 제한된 종료 기록뿐이다.

bootstrap은 DB·broker 연결·worker의 시작과 종료를 소유한다. 요청 단위 객체에 lease owner나 cursor key를 새로 만들지 않는다. 기존 CORS·body size·decode 엄격성·시간 제한·alias 처리·파일 권한을 adapter로 이동할 때 유지한다.

## 6. Flutter 목표 구조와 요청 수명

```text
lib/
  main.dart
  app/                            composition, navigation, session coordination
  core/domain/                    실제 공유 money/value
  core/data/                      bounded HTTP/UTF-8 transport
  core/presentation/              theme, 접근성, 공통 표시 위젯
  features/
    portfolio/ ledger/ market_data/ connections/ watchlist/ research/ paper/
      domain/                     값과 해당 feature의 consumer port
      application/                상태 전이, 요청 순서·중복·취소 정책
      data/                       REST 구현과 strict codec
      presentation/               페이지, 위젯, Flutter 수명 adapter
```

모든 feature에 네 폴더를 빈 채로 만들지 않는다. application이 필요 없는 단순 읽기는 data→domain과 presentation 소비만으로 끝낸다. domain/application은 Flutter widget과 HTTP client를 import하지 않는다. pure Dart 상태 알림 방식 하나를 재사용하고 presentation에서 Flutter에 연결한다. feature 간 data/presentation 직접 참조는 금지하며 app이 탐색 callback과 교차 화면 조정을 소유한다.

| feature | 기존 API 책임 |
| --- | --- |
| portfolio | snapshot, current/holding valuation, XIRR, TWR, checkpoints, benchmarks |
| ledger | instrument ledger, activities, preview, apply |
| market_data | candles, owner-data preview/apply |
| connections | status, reconciliation, orders |
| watchlist | 조회와 revision 기반 변경 |
| research | research report, public research |
| paper | monitor, promotion report |

각 repository는 이 endpoint의 실제 구현과 codec을 이동해 소유한다. 기존 `OmniApi`를 내부에서 그대로 호출하는 일곱 wrapper를 만들지 않는다. 모든 caller와 fake를 옮긴 뒤 omnibus API/model을 제거한다. 내부 Dart API는 바꿀 수 있으나 HTTP wire, 사용자 표시, accessibility, source identity는 보존한다.

| 상태 | 수명·소유자 | 보존할 동작 |
| --- | --- | --- |
| portfolio/watchlist 세션 | app composition | route 변경만으로 observation·revision·진행 상태를 초기화하지 않음 |
| import apply, owner-data apply | app session controller | 제출에 캡처된 CSV/preview/idempotency key와 미확정 결과를 페이지 종료 후에도 보존 |
| 편집 CSV/form/preview·cursor·request epoch | 해당 page/application owner | 화면 폐기 후 늦은 결과가 새 화면 상태를 덮지 않음 |
| chart/report/public research | 해당 route | source ID, 표시 범위, 보고서 identity, 선택 상태 유지 |
| 전체 observation | 해당 feature controller | 실패하면 직전 observation 유지; 성공한 unavailable 응답은 새 상태로 교체 |

watchlist는 ACK revision을 확인한 뒤 가격을 갱신한다. CAS 결과가 불명확하면 적절한 reload 전까지 다음 쓰기를 막는다. ledger pagination은 continuation과 stale epoch 조건을 유지한다. 단순 `FutureBuilder` 재생성으로 이러한 상태를 대체하지 않는다.

`OwnerDataPreview.summary`의 느슨한 Json은 검증된 security-price/FX/benchmark 요약 타입으로 바꾼다. 제출에 필요한 원본 captured input은 따로 보존한다. `api is RestOmniApi`에 의존하는 연결 표시를 제거하고 composition에서 표시 정보를 명시적으로 전달한다. 수동 입력의 금액 계산은 domain, CSV 작성은 data, 한국어 label은 presentation이 소유한다.

공통 transport는 byte 제한·취소·오류 정규화만 맡는다. endpoint별 기존 정책을 하나로 통일하지 않는다: watchlist 요청 16 KiB/응답 128 KiB/40초, owner import 입력 1 MiB와 기존 응답 제한/40초, checkpoints 3 MiB/40초, public research의 2단계 15초와 abort 정책을 보존한다. 나머지는 기존 `api.dart`의 정책을 이동한 뒤 parity test로 확인한다.

## 7. Python 연구 경계

```text
omni_research/
  domain/                         Bar/Order, Decimal/Fraction, backtest, SMA, fold, paper fill
  application/                    experiment, proposals, captured research, artifact 생성
  adapters/inputs/                CSV/config/native capture decode
  adapters/files/                 bounded descriptor read, durable publication
  adapters/signal_stream/         watch pipe, stdout/exit 처리
  engine.py improve.py ...        기존 public API의 얇은 composition facade
  __main__.py *_cli.py            argparse와 composition
```

domain은 Path, 파일, JSON, 환경 변수, 프로세스, 현재 시각을 사용하지 않는다. application은 검증된 입력으로 결과·artifact를 만들며 실제 읽기/쓰기는 하지 않는다. artifact canonical encoding/hash는 순수 연산으로 application에서 단일 소유한다. input decoder가 domain 값을 만든다. CLI는 파일 adapter와 application을 조합한다.

`engine.build_manifest`, `improve.run_experiment/simulate_fold`, capture experiment와 signal proposal/bundle의 기존 공개 import 및 네 CLI 경로를 유지한다. facade는 I/O 조합만 하며 계산을 복제하지 않는다. 내부 코드는 이 facade를 import하지 않는다. 기존 테스트의 root global monkeypatch는 실제 책임의 주입 지점으로 옮긴다.

다음은 변경 불가 계약이다.

- legacy v1 Decimal과 v2 Fraction의 의미를 섞지 않는다. finite money, ratio 28자리 half-even, whole capacity, delay와 tie-break를 유지한다.
- canonical hash의 UTF-8, `ensure_ascii=False`, sorted compact JSON, self-hash 제외 범위를 유지한다. raw input hash는 CRLF·공백을 포함한 원본 bytes를 대상으로 한다.
- captured row/provider_time을 보존한다. YYYYMMDD를 확인된 체결 시각으로 승격하지 않는다. shared evaluator 사용만으로 capture report를 등록 가능한 candidate/proposal로 바꾸지 않는다.
- 파일 descriptor의 regular-file 확인, FIFO nonblocking, 크기 제한, duplicate key·unsafe number 거부, private parent, O_NOFOLLOW, exclusive create, fsync, 기존 bytes 비교, 실패 cleanup을 유지한다.
- durable archive 완료 후 stdout으로 출력하는 순서를 유지한다. 임의 난수는 결과 선택에 도입하지 않는다. 임시 파일명의 난수는 외부 adapter에 남긴다.

기존 `engine.build_manifest`는 hash용 읽기와 bar용 읽기가 분리돼 있다. 이 설계는 단일 snapshot이라고 주장하지 않는다. TOCTOU 개선은 별도 재현·계약 변경으로 처리하며, 파일 이동과 함께 조용히 의미를 바꾸지 않는다.

## 8. 실제 파일 이동과 영향 범위

범위의 단위는 파일 전체가 아니라 책임이다. 아래 큰 파일은 규칙·SQL·transport를 나누며 테스트도 같은 owner로 이동한다.

| 현재 위치 | 목표 책임·위치 | 함께 바꿀 소비자 |
| --- | --- | --- |
| core `core.go` 값/preview/apply/replay | ledgerdomain, application/ledger, sqlite | HTTP/CLI, ledger tests, fixtures 경로 |
| `core.go` DB open/schema/backup/restore | sqlite, application/maintenance, files | root composition, embed, backup/migration tests |
| `core.go` routes/CORS/errors, `main.go` | httpapi/cli/bootstrap | OpenAPI parity, root build, smoke, container entrypoint |
| ledger activity/replay/export/instrument ledger | ledger + sqlite/httpapi | cursor/CSV/privacy tests와 client codecs |
| cash/holding/current/owner price valuation, portfolio XIRR/TWR/benchmarks | portfolio domain/application + sqlite 조회 | feature repository·평가 fixtures |
| market data/FX/price/listing/watchlist, owner imports | marketdata + sqlite/kiwoom/files | ledger identity snapshot, CAS·import tests |
| order state/read, execution authority, broker snapshot/reconciliation | execution + coordinator + sqlite/kiwoom | order hash/recovery, CLI와 connection read |
| `paper_*.go`, strategy registry | research/execution coordinator + 순수 기존 domain | paper accounting/policy/promotion/runner와 Python integration |
| `public_research.go` | research validation/application + files/httpapi | closed-shape·bounded read·public research UI |
| Flutter `app.dart` shell/theme/common UI | app + core/presentation | navigation, widget keys, accessibility tests |
| `app.dart` overview/holdings/chart/activity/connections | 각 feature presentation/application | session owners, route epochs, 모든 page caller |
| Flutter `api.dart`, `models.dart`, 별도 report/model 파일 | 해당 feature data/domain | strict parser, fake, fixture, widgets/integration tests |
| import/watchlist/owner-data controller와 manual/csv 파일 | feature application/domain/data | pending write·cancel·file picker tests |
| Python engine/improve/paper_fill/capture/signal | domain/application/adapters + 호환 facade | unit monkeypatch, CLI smoke, Go subprocess integration |
| Makefile, CI, Containerfile/Dockerfile, compose, tools/tests | 기존 진입점 유지, 경로 검사 재귀화 | root check/smoke, release scripts, resource cleanup |
| README/docs/contracts/gates/license/identity | 설계 링크만 변경; 계약 내용 보존 | 문서 상대 링크, compatibility identifiers |

기존 Go의 `../../contracts` 같은 cwd 상대 fixture 참조와 root `go build`를 package 이동 시 함께 수정한다. 테스트 support 한 곳이 저장소 fixture root를 해결하고 각 테스트는 기존 fixture를 재사용한다. Flutter의 다른 테스트 파일을 import해 helper를 얻는 패턴은 `test/support`로 옮긴다. fake는 테스트에서 여러 좁은 port를 구현할 수 있지만 production omnibus API를 되살리지 않는다.

Go module `omni-folio/services/core`, Dart package `omni_folio_client`, Python namespace `omni_research`, DB/keychain/app/env 식별자, executable `omni-core`는 유지한다. root `goal.md`는 계속 local-only다. Mimir Actions, GitHub 공개 범위, license와 branch protection은 이 리팩토링에서 변경하지 않는다.

## 9. 기계적으로 막을 잘못된 의존성

| 영역 | 통과 조건 | 음성 검증 예 |
| --- | --- | --- |
| Go | 모든 하위 production source의 import graph 검사. domain allowlist, application→adapter 금지, bootstrap 생성자 제한 | 중첩 domain 파일의 database/sql import, 다른 package의 dispatch 생성 각각 실패 |
| Flutter | 전체 lib의 import/export/part URI 해석, 상대·package URI 정규화. feature data/presentation 횡단 금지 | 상대 경로를 이용한 다른 feature data import도 실패 |
| Python | rglob + AST Import/ImportFrom 분석, relative import 해석. 순수 계층 I/O와 내부→facade 금지 | 하위 패키지의 socket/subprocess/sqlite3 또는 root engine import 실패 |
| 공통 | 실제 파일 목록에서 검사 대상을 도출; 무검사 새 폴더 방지 | 신규 nested source가 gate 대상임을 확인 |

정적 검사는 동적 import와 reflection의 모든 동작을 증명하지 않는다. domain/application에서 동적 import/FFI/우회 entrypoint를 추가하지 않으며 위반은 리뷰 대상이다. allowlist를 통째로 넓혀 실패를 해결하지 않는다. 기존 여섯 Go 순수 패키지 검사만 통과했다고 전체 경계가 검증됐다고 보고하지 않는다.

## 10. 단계별 전환과 종료 기준

각 단계는 기능·caller·테스트를 함께 이동하고 작동 가능한 commit으로 끝낸다. 운영 코드에서 신규 구현이 레거시 구현으로 다시 돌아가는 중복 경로를 남기지 않는다. 아래 순서는 의존 순서이며 기능 출시 일정이 아니다.

| 단계 | 작업 | 종료 증거 |
| --- | --- | --- |
| R0 계약 고정 | 현재 API/CLI/fixture/migration/hash·테스트 인벤토리와 dirty state 확보 | baseline commit, 각 계약 hash, 기존 실패와 변경 소유권 기록 |
| R1 공통 경계 | bootstrap, fixture support, 각 언어 recursive architecture gate | root 실행 유지, 금지 import 음성 검증, 빈 폴더만 늘리지 않음 |
| R2 읽기와 계산 | portfolio/market read, Python 순수 계산·artifact, Flutter 해당 codec/repository | 정밀 값·unavailable/error·JSON/hash parity |
| R3 원장과 입력 | ledger/import/owner-data/watchlist, Flutter session controllers | preview/apply idempotency, revision/CAS, route 이탈과 늦은 응답 검증 |
| R4 실행과 모의 | typed transaction scopes, dispatch/authority/paper/runner/promotion | 아래 장애 표의 모든 경계, 기존 recovery/hash와 Go↔Python 결과 일치 |
| R5 유지보수와 표면 | migration/backup/files, HTTP/CLI, 잔여 Flutter pages와 Python facade | restore/integrity, 전체 route·CLI/fixture, strict parser, 화면·접근성 회귀 |
| R6 통합·삭제 | 미사용 main 로직·omnibus 모델/API 제거, 문서/CI 연결 | root check/smoke, release 경로, Podman 검증, 변경 후 인벤토리와 최종 diff |

R2의 Go/Flutter/Python은 계약이 고정된 부분만 독립 작업할 수 있다. 공유 signatures, contracts, Makefile, CI는 통합 owner 한 명이 소유한다. R4는 SQLite 연결·scope·coordinator를 서로 다른 작업자가 독립적으로 재설계하지 않는다. 완료된 단계의 검사 결과는 소스가 바뀌지 않으면 재사용한다.

### 장애·동시성 acceptance

| 조건 | 반드시 관찰할 결과 |
| --- | --- |
| 예약 후 dispatch 기록 전 오류 또는 commit 실패 | 예약과 이벤트가 함께 rollback, 외부 submit 0회 |
| dispatch commit 직후 crash 또는 submit timeout | durable dispatched 남음, 같은 order 재실행이 submit하지 않음 |
| broker ACK 후 outcome commit 실패 | 재제출 금지, reconciliation으로 회복 가능한 기록 |
| fence 만료/owner 교체/동시 호출 | stale owner 거절, 중복 예약·dispatch 없음 |
| paper fill 중 실패 또는 동일 fill 재시도 | 중복 capitalized entry와 replay 이중 증가 없음 |
| policy halt 일부 기록 실패 | decision/halt/selection/lease 모두 rollback |
| heartbeat 한쪽 갱신 실패 | 양쪽 lease 모두 이전 상태 |
| promotion 계산 취소·crash·선택 변경 | bounded finish 또는 PENDING, stale 후보 승격 금지 |
| import 적용 중 route 종료·재진입 | captured input/key와 미확정 결과 보존, 자동 중복 apply 없음 |
| 이전 페이지 요청의 늦은 응답 | 새 page/session state를 잘못 덮지 않음 |
| strict decode/oversize/invalid file·symlink | 기존 오류와 resource 정리, 부분 산출물 미공개 |
| backup/restore 중 실패 | 원본 DB와 기존 성공 백업 보존, 검증되지 않은 복구 성공 보고 없음 |

이 표는 새로운 주문 정책이 아니라 기존 보장의 회귀 기준이다. 누락된 실패 주입 테스트만 추가하고 기존 테스트를 구현 모양에 맞춰 약화하지 않는다.

## 11. 검증과 롤백

설계 문서 변경만 있는 단계에서는 상대 링크·source anchor·모순·diff 검사를 수행한다. runtime suite를 실행하지 않고 실행했다고 보고하지 않는다. 구현 시에는 변경된 owner의 집중 테스트를 먼저 실행하고 최종 소스 상태에서 root `make check`와 `make smoke`를 각각 한 번 수행한다. 실패하면 해당 원인부터 수정하며 같은 전체 suite를 반복하지 않는다.

- Go: 전체 기존 테스트의 assertion 보존, exact/FIFO/hash/migration/backup/recovery, 실제 SQLite transaction 통합과 교착 여부, architecture gate.
- Flutter: analyzer, widget/unit, 기존 integration profile, route lifetime·strict codec, 해당 플랫폼 release 경로. iOS는 기존 `make client-ios-release`와 관련 helper 계약을 따른다.
- Python: unittest 전체와 boundary gate, CLI artifact byte/hash, Go의 strategy registry/paper bundle/pipeline/captured candle 연동.
- 계약: OpenAPI·JSON schema·fixture·SQL migration bytes를 R0와 비교한다. 동일 결과라도 hash 대상 byte가 바뀌면 실패다. 원래 migration 7/20/27의 Go 측 증명·재구성·index 처리도 유지한다.
- 컨테이너: **Podman 사용**, Docker Desktop을 실행하지 않는다. 기존 Dockerfile/Compose 형식은 파일 형식이므로 유지할 수 있다. Podman machine/compose provider를 읽기 전용으로 확인한 후 task-owned 이름·DB·포트로 build와 smoke한다. unavailable이면 native 통과와 container 미검증을 구분한다.
- 자원: 기존 test ownership/cleanup target 사용. 작업 전용 임시 로그·DB·컨테이너·이미지·볼륨만 종료·사용 상태 확인 후 정리한다. 공유 cache나 다른 worktree는 삭제하지 않는다.

이번 refactor는 schema·저장 bytes·외부 계약을 바꾸지 않으므로 코드 commit을 되돌리는 방식이 기본 rollback이다. **실행 중인 writer를 먼저 정상 종료하고**, 동일 DB의 호환성을 확인한 뒤 이전 binary로 돌아간다. 부분 적용 DB를 초기화하거나 history를 다시 쓰지 않는다. 실제 schema 변경이 필요해지면 이 설계의 전제가 깨진 것이므로 별도 migration/backup/restore 설계로 분리한다.

## 12. 설계 변경을 다시 검토할 조건

다음 중 하나가 생기면 임시 wrapper나 범용 interface로 덮지 않고 해당 설계를 수정한다: 두 사용 사례에서 같은 우회 의존성이 반복됨, transaction scope가 SQL API로 확대됨, domain이 환경·파일·네트워크를 요구함, Flutter page 이동 때문에 쓰기 상태가 유실됨, Python facade 안에 계산이 다시 들어감.

문서와 구현 사이의 승인된 차이는 이 문서의 해당 계약과 아래 기록에 함께 반영한다. 실패를 숨기기 위해 acceptance를 낮추지 않는다.

| 항목 | 처리 |
| --- | --- |
| 외부 계약·금액·원자성·권한 변경 | 단순 refactor에서 제외하고 별도 명시 결정 |
| 내부 명칭·파일 분할 변경 | 책임·의존성·검증 계약이 같으면 구현 리뷰에서 수용 가능 |
| 현재 알려진 build_manifest 이중 읽기 | 별도 버그로 분리; snapshot 보장 주장 금지 |
| 구현 조정 이력 | 아직 승인된 조정 없음. 실제 적용과 검증은 단계별 결과로 확인한다. |

첫 구현 단위는 R0 근거를 현재 checkout과 대조한 뒤 R1의 실행 진입점·재귀 의존성 검사를 연결하는 것이다. 설계 검토 완료는 구현·CI·실제 키움 연결 검증 완료를 뜻하지 않는다.

## 13. 설계 검토 결과와 증거의 범위

2026-11-05에 `pstack:architect`의 대안 비교, 독립 아키텍처 리뷰, `pstack:interrogate`의 반례 검증을 적용했다. prose는 `clear-writing`, 외부 API 보존 원칙은 `postman:api-engineer`를 적용했다. 검토 결과가 런타임의 올바름을 증명하는 것은 아니다.

| 검토에서 발견한 공백 | 최종 설계에 반영한 결정 |
| --- | --- |
| paper admission 쓰기 포트 누락 | signal·intent·authorization·event pair·ACK를 동일 scope로 명시 |
| dispatch capability 생성과 import 방향의 모순 | adapter concrete factory + application consumer port + bootstrap 등록 제약 |
| 분리된 identity 조회 후 revoke 경쟁 | 거래 grant/epoch의 authority DB 소유와 revoke/dispatch commit 순서 |
| PostgreSQL 계좌 잠금으로 전역 event 순서 훼손 | 첫 전환의 전역 writer 잠금, 이후 scope/proof migration 분리 |
| tenant backfill만으로 전역 policy가 격리된다는 오해 | selection/runner/policy/ledger/watchlist scope migration 후 다중 tenant 개방 |
| SQLite cloud 계획과 서버 계획 충돌 | P2a 단일 SQLite 서버와 P2b PostgreSQL 서버로 분리 |
| Kafka 단독·RabbitMQ 단독의 역할 공백 | publish/replay/job/authority의 profile별 구현 조합 명시 |
| Kafka 뒤 record 선완료 후 앞 record 누락 | 초기 partition 직렬 처리와 완료 prefix까지만 offset commit |
| Rabbit confirm 이후 unroutable/비영속 메시지 유실 | destination별 routing·mandatory return·persistence·confirm 조건 |

독립 리뷰의 최종 보완본에서는 미해소 blocking/high를 발견하지 못했다. Kafka/RabbitMQ 두 반례는 순서·장애 모델을 실행해 재현하고 공식 문서의 의미와 대조했다. 실제 broker를 실행한 결과가 아니다. Python `build_manifest`는 실제 함수에 읽기마다 바뀌는 입력을 주어 두 번 읽기와 hash/계산 입력 불일치를 재현했다. 이는 기존 동작이고 이 문서가 보장 범위에서 제외한 한계다. 후속 정합성 변경에서는 한 번 읽은 immutable bytes로 hash와 decode를 수행하도록 별도 실패 테스트·수정 commit을 계획해야 하며, 그 전에는 변경 가능한 파일의 snapshot 일관성을 주장하지 않는다.

문서 상대 링크와 fence, 기존 JSON 계약 파싱, 추적 중인 계약·migration hash를 검사한다. Postman CLI 1.75.0에는 skill의 `status` 명령이 없어 해당 setup 검사는 불가했다. 새 Postman workspace를 만들거나 기존 OpenAPI를 복제하지 않았다. 현재 설계의 근거는 저장소 계약과 원본 코드다.

전체 runtime suite, Flutter 실기기, DB/MQ/IdP/Redis 연동, 부하·복구 drill은 설계 단계에서 실행하지 않았다. 실제 지원 프로필은 해당 구현과 acceptance를 통과한 뒤에만 확정한다.


## 14. 중단 기록과 재개 지점 — 2026-11-05

사용자가 진행 중인 단위를 정리한 뒤 중단하고, 구현된 부분을 `main`에 통합하도록 요청했다. **전체 리팩토링 완료가 아니다. 명시적인 재개 요청 전에는 후속 구현을 시작하지 않는다.** 원격 push·배포·실제 키움 연결은 수행하지 않았다.

### main에 통합한 범위

| 범위 | main 커밋 | 검증된 내용 |
| --- | --- | --- |
| Python 기존 산출물 특성화 | `80ebc4a` | 기존 CLI 6종 byte hash와 이중 읽기 동작 고정 |
| Python domain/application/adapters | `2e8dc6b` | 66개 검사, 45개 소스 구문 검사, 6개 CLI byte hash 일치 |
| Go 원장 normalization/FIFO replay | `c2fedda` | import·FIFO·replay·golden backup·chronology 집중 검사 |
| Go 포트폴리오 계산 | `0b13a7a` | XIRR·TWR·benchmark 계산 및 기존 assertion 보존 |
| Go 원장 use case/SQLite scope | `0e5f0cc` | typed Read/Write scope, callback·commit·cancel·panic rollback, HTTP 오류·golden 집중 검사와 vet |

외부 JSON 계약과 SQL migration 파일은 설계 기준 커밋 `92034c5` 대비 변경하지 않았다. Go 루트의 나머지 업무·HTTP·CLI·bootstrap 분리, Flutter 실제 경계 이전, 최종 독립 구현 리뷰는 아직 남아 있다. 원장 receipt 재사용은 새 Write callback 계약상 쓰기 없이 읽기 transaction을 commit한 뒤 반환한다. 기존 구현은 이 경로를 rollback으로 닫았으며, 응답·멱등성 보존은 기존 검사로 확인했다.

### main에 병합하지 않은 체크포인트

| 브랜치 / 커밋 | 보존 경로 | 남은 문제 |
| --- | --- | --- |
| `refactor/client-boundaries` / `8e8177d` | `/tmp/kairos-client-refactor-20261104` | 집중 검사 72 통과·2 실패. 공통 helper의 잘못된 소유/역참조, architecture lexer의 중첩 interpolation 해석, typed summary equality 수정 필요 |
| `refactor/kiwoom-adapter-owned` / `32f40aa` | `/tmp/kairos-kiwoom-adapter-20261105` | adapter 단위 검사만 통과. root caller/type 연결과 기존 구현 제거 미완료. 그대로 cherry-pick하면 root가 빌드 가능한 상태가 아니다 |

Flutter의 새 `data/` 소스는 전역 ignore 패턴에 걸려 정확한 파일만 force-stage하여 체크포인트에 보존했다. 재개 시 저장소 `.gitignore`에 client source 예외를 추가해야 한다. 기존 프로젝트 데이터 경로의 ignore는 유지한다. Flutter 112개 변경 파일 중 production Dart 78개가 추적됐는지 확인했다.

### 재개 순서

1. 저장소 `/Users/yousang/Desktop/workspaces/toy-projects/Kairos`에서 `git status --short`, `git log -8 --oneline`, `git worktree list`로 이 기록 이후의 변경을 먼저 확인한다. 기존 worktree를 재사용하고 다른 변경을 덮어쓰지 않는다.
2. Flutter checkpoint에서 `output/client-refactor-pause-20261105/PAUSED.md`의 실패 근거를 읽는다. `decodeObject`를 공통 data codec으로, timestamp 계산을 pure domain helper로 옮기고 lexer·summary 검사와 `.gitignore`를 수정한다. 실패 집중 검사부터 재실행한 뒤 전체 client 검사로 확장한다.
3. Kiwoom checkpoint는 최신 main의 원장 변경과 비교한 뒤 계속한다. `NewKiwoomClientWithRuntime`, capture batch, realtime/history 호출자를 새 adapter로 연결하고 root 중복 구현을 제거한다. `orderdomain.Intent`의 의존성 gate와 중복 schema 상수의 정본을 정리한다. private token/credential 상태를 공개하는 방식으로 연결하지 않는다.
4. Go의 남은 application/adapter/bootstrap 경계를 기존 설계의 원자성·순서·금액·권한 계약에 따라 완료한다. root entrypoint thinness 검사는 이 이전 완료 후 적용한다.
5. 최종 diff에 대해 저장소 필수 검사, 독립 구현 리뷰, smoke, Podman 및 필요한 release 검증을 수행한다. 사용자 중단으로 이 단계까지 완료하지 않았으며, 이후 통과 전에는 전체 구현·운영 준비 완료를 주장하지 않는다.

로컬 상세 근거는 `docs/_internal/skill-outputs/whole-refactor-20261104/`와 `output/client-refactor-pause-20261105/`에 보존한다. 두 경로는 Git 제외 경로다. 작업 소유 캐시는 정리하되 재개를 위한 worktree·체크포인트·검증 근거와 기존 공유 캐시는 보존한다.


### 중단 시점의 최종 통합 검증과 정리

`main`의 위 구현과 재귀 의존성 gate/format target에 대해 asdf의 Go 1.24.5, Flutter 3.47.1-stable, Python 3.14.5를 지정한 `make check`가 exit 0으로 완료됐다. 형식 검사·Go vet·Flutter analyze·Python 구문 검사, Go 전체 패키지(root 543.443초), main의 Flutter 483개 검사, Python 66개 검사, JSON 계약 48개 검증이 통과했다. **main에 병합하지 않은 Flutter WIP의 실패 2개가 해결됐다는 뜻은 아니다.** 검증 전문은 로컬 `docs/_internal/skill-outputs/whole-refactor-20261104/main-check.log`에 보존했다.

작업 전용 Go 캐시 197,448 KiB와 Flutter WIP 캐시 60,612 KiB를 삭제했다. 독립 portfolio 작업에서도 전용 캐시 150,356 KiB를 정리했다. Python bytecode 잔여는 없었으며, 임시 추출 스크립트와 복사 완료된 임시 로그를 정리했다. make의 소유 테스트 임시 루트 제거를 확인했다. 기존 main의 Flutter build/.dart_tool과 공유·전역 캐시는 기존 자원이므로 보존했다. 이번 작업은 Podman 컨테이너·이미지·볼륨을 생성하지 않았다.

모든 구현 에이전트는 중단했고 활성 작업 handle은 남기지 않는다. 재개용 소스 worktree와 WIP commit은 의도적으로 보존한다. 최종 독립 구현 리뷰·smoke·Podman/release 검사·CI·실제 키움 연결은 미실행이며, 다음 재개에서 필요한 검증으로 남긴다.

## 15. 업무 모듈과 데이터 소유권 보완 설계 — 2026-11-09

### 15.1 범위와 적용 우선순위

이번 요청은 **설계만** 수행한다. 확인한 checkout은 `main@f6a6075`이며 작업 트리는 깨끗했다. 14절의 구현 중단 기록은 과거 사실로 보존한다. 이 절은 사용자의 여섯 가지 요구를 기존 설계에 연결한 후속 제안이다. 아직 구현·배포됐거나 사용자가 세부안을 승인한 것으로 기록하지 않는다.

1–13절의 금액·wire·트랜잭션·수명·복구 계약과 플랫폼 확장 설계는 유지한다. 이 절은 **후속 구현의 물리적 모듈 배치와 접근 제한**을 구체화한다. 3절의 책임 영역은 모두 독립 모듈이라는 뜻이 아니다. 5절의 전역 application/adapter 배치는 아래 모듈별 소유 배치로 옮기는 제안이며, 전역 SQLite adapter에 모든 테이블 접근을 계속 모으는 안을 대체한다. 3절의 읽기 SQL join 허용은 같은 owner 내부 join으로 제한하며 다른 owner 데이터는 공개 조회 계약으로 받는다. 6절의 Flutter 7 feature는 화면 경계다. 서버의 데이터 소유 모듈과 일대일 대응시키지 않는다.

기존 임시 worktree들은 Git 목록에 `prunable`로 남고 실제 경로는 사라졌다. `8e8177d`, `32f40aa` 커밋 객체는 확인했다. 구현 재개 시 해당 커밋에서 경로를 복원한 뒤 최신 main과 비교한다. 기존 경로가 있다고 가정하거나 worktree 목록을 먼저 prune하지 않는다.

### 15.2 관측한 구조와 보존할 이유

- `internal/application/ledger/{service,ports,snapshot}.go`와 `internal/adapters/sqlite/ledger.go`는 실제 이식됐다. 다만 application이 CSV를 파싱하고 HTTP status를 가진 `fault.Error`를 반환한다. 레이어 이름만으로 최종 구조를 충족하지 않는다. CSV decoder와 HTTP 오류 매핑을 각 adapter로 옮겨야 한다.
- `paper_local.go`는 strategy evidence를 SQL로 읽고 runner·authority·order·paper 회계를 조합한다. `paper_performance_policy.go`는 policy 기록, armed account halt, selection rollback을 묶는다. `paper_promotion.go`는 claim과 selection의 오래됨을 검사한다. 이 상태를 각각 독립 모듈로 잘라 transaction을 나누면 안 된다.
- `internal/architecture/dependencies_test.go`는 재귀 domain import와 일부 application/dispatch 제약을 검사한다. 모듈별 공개 API·모든 테이블 접근·전체 순환 의존 검사는 아직 보장하지 않는다.
- Flutter는 main의 `app.dart/api.dart/models.dart`에 넓은 결합이 남아 있다. Python은 domain/application/adapters 이식이 반영됐지만 파일 snapshot 이중 읽기 문제는 별도 보류다.
- 제품의 핵심은 정보→연구→추천/허용된 실행→성과·복기다. 임의 코드를 설치·실행하는 플러그인 제품이라는 근거는 없다. 이번에 plugin loader나 범용 실행 호스트를 추가하지 않는다.

### 15.3 두 구조의 비교와 선택 이유

| 후보 | 동작 방식 | 장점 | 비용·실패 조건 |
| --- | --- | --- | --- |
| A: 거래를 하나의 모듈로 묶고 내부 업무 영역 분리 | 주문·권한·예약·선택·paper를 한 owner의 transaction으로 처리 | 현재 원자성 보존, 외부 API가 transaction 조립을 숨김 | 거래 모듈이 상대적으로 크다. 내부 영역의 SQL·공개 타입을 계속 제한해야 함 |
| B: 주문·위험·선택·paper를 독립 모듈로 분리 | 각 owner가 쓰고 공개 프로토콜로 협력 | 개별 변경·서비스 분리 경계가 작음 | 현재 atomic halt/rollback/admission을 유지하려면 분산 프로토콜 또는 외부 transaction 주입이 필요. 전자는 업무 의미 변경, 후자는 내부 구현 노출 |

**설계 제안은 A다.** 사용자가 요구한 데이터 소유권과 기존 원자성을 함께 만족하는 작은 완전한 전환이다. 미래 서비스 분리는 거래 모듈 전체부터 가능하다. 거래 내부 영역을 서비스로 분리하려면 현재 원자적 계약 변경과 복구 프로토콜을 별도 승인·검증해야 한다. 지금 이벤트 버스로 이 결합을 감추지 않는다.

### 15.4 모듈, 공개 계약, 테이블의 단일 소유자

아래 공개 연산명은 목표 내부 API다. 기존 HTTP/CLI 경로를 추가·변경하는 선언이 아니다. DTO는 명령·결과에 필요한 값만 담고 SQL row, transaction, ORM 모델, HTTP 객체, provider raw payload를 노출하지 않는다.

| 모듈 | 숨기는 업무 책임 | 공개 명령·조회 예 | 소유 테이블/영속 상태 |
| --- | --- | --- | --- |
| ledger | 원장 정규화·중복·정정·revision·FIFO·증권사 잔고 대조 | PreviewImport, ApplyImport, Snapshot, ValuationFacts, AdmittedPriceIdentities, Activity, InstrumentActivity, RecordBrokerSnapshot, Reconciliation | events, ledger_meta, previews, receipts, ledger_replay_state, ledger_replay_transition, broker_snapshots, broker_snapshot_reconciliations |
| marketdata | 상장·가격·환율·입력 버전·출처·시간, ledger 공개 자격에 근거한 사용자 가격 등록 | ResolveListing, CapturePrice, ImportOwnerSeries, PriceSnapshot, FXSnapshot, BenchmarkSnapshot | fx_observations, security_price_observations, instrument_listing_events, owner_fx_series_versions, owner_price_series_versions, benchmark_series_versions |
| watchlist | 관심 종목 설정과 revision CAS | Read, Replace(expectedRevision, items) | watchlist_state |
| trading | 주문·authority·예약·runner·전략 채택·paper 회계·성과·승격 | RegisterEvidence, SelectStrategy, AdmitPaperSignal, RecordPaperFill, SubmitSyntheticLimit, ReconcileKnownOrder, ApplyPerformancePolicy, PromotePaperCandidate, Halt, Monitor | order_idempotency, order_events, execution_authority_events, risk_reservations, strategy_research_evidence, strategy_selection_events, paper_evaluation_events, paper_accounting_sessions, paper_market_bar_observations, paper_signal_events, paper_execution_authorizations, paper_performance_events, paper_strategy_performance_events, paper_performance_policy_events, paper_runner_leases, paper_promotion_claims, paper_promotion_decisions |
| portfolio | 평가 가능성·현금/보유 평가·XIRR/TWR·비교 | CurrentValuation, XIRR, TWR, CompareBenchmarks | 권위 테이블 없음. 입력 revision이 포함된 계산 결과만 소유 |
| research | 재현 가능한 계산·후보 비교·proposal 생성·공개 정보 입력 | BuildManifest, RunExperiment, BuildProposal, EvaluateCapture | hash가 붙은 연구 artifact와 입력 reference. trading 테이블은 소유하지 않음 |
| maintenance | schema·정합성 검사·백업/복원 | Verify, ExportBackup, RestoreBackup, Migrate | schema_migrations의 유일한 writer와 backup manifest. 일반 업무 테이블의 새 writer가 아님 |


001–027의 CREATE TABLE을 조사했다. `events_v5/v8/v9`, `order_idempotency_v7`, `*_new`는 migration 중 사용하는 교체 이름이며 최종 테이블과 같은 owner에 속한다. 새 table/view/trigger의 owner가 미정이면 검사를 실패시킨다. maintenance는 migration/restore 순서를 조율하고 각 module owner는 schema와 proof의 의미를 정의한다.

watchlist는 시장 관측 사실과 변경 이유가 다르고 독립 CAS로 갱신할 수 있어 분리한다. trading의 시장 bar는 외부 시세의 정본이 아니라 해당 paper 실행에서 채택한 immutable evidence다. 등록한 research artifact도 trading이 수락하고 hash를 검증한 증거다. 변경 가능한 연구 파일을 공유 상태로 참조하지 않는다.

### 15.5 배치와 의존 방향

목표 Go 구조는 `internal/modules/<owner>/api.go`, `internal/modules/<owner>/internal/domain/`, `internal/modules/<owner>/internal/application/`, `internal/modules/<owner>/internal/adapters/{sqlite,http,cli,provider,files}/`다. 공개 DTO와 작은 업무 capability 계약은 `internal/modules/<owner>/contracts/`에 둔다. private application은 공개 입출력 타입으로 자신의 contracts를 사용하고, 외부 업무 의존은 소비자 port로 선언하며 domain은 contracts/transport에 의존하지 않는다. module root의 `api.go`/`compose.go`는 구성 진입점을 제공하고 private application이 구현한 contracts의 업무 capability를 반환한다. contracts는 DB/HTTP/private 타입을 import하지 않으며 root를 역으로 import하지 않는다. 모든 함수를 감싸는 facade는 만들지 않는다. `internal/composition/`이 설정·생성·주입·기동·close를 조율하고 main은 인자와 종료 결과를 처리한다.

Go의 nested `internal` 규칙으로 다른 module의 `<owner>/internal/...` import를 컴파일 단계에서 차단한다. composition도 비공개 구현을 직접 import하지 않는다. 각 module의 공개 `Compose` 진입점이 비공개 adapter를 생성하고 composition에서 받은 설정·의존 포트를 연결한다. Compose는 배선만 담당하며 업무 규칙을 갖지 않는다. application/domain에서 Compose 또는 module root로 역방향 import하는 것은 금지한다.

아래 화살표의 실제 업무 import 대상은 상대 owner의 `contracts` package이며 root Compose는 composition에서만 사용한다. 공개 계약의 import 방향은 `portfolio → ledger, marketdata`, `marketdata → ledger`, `trading → marketdata`, `watchlist → marketdata`다. watchlist→marketdata는 기존 관심 종목 quote 조회에 필요하다. listing 검증을 새로 추가하는 연결로 해석하지 않는다. ledger는 다른 업무 module을 import하지 않는다. marketdata는 사용자 가격의 원장 종목 자격 검증에 한해 ledger의 공개 조회 계약을 소비한다. ledger→marketdata 역방향 의존은 금지한다. research와 trading은 기존 version이 붙은 artifact로 연결하고 연구 측은 trading 내부를 import하지 않는다. maintenance는 각 owner의 공개 정합성/backup 계약을 호출한다. owner에서 maintenance로의 의존은 금지한다. shared에는 exact 계산과 작은 계약값만 두며 공통 Service/범용 Repository는 두지 않는다.

composition은 DB pool의 수명을 소유하고 각 module의 Compose에 같은 pool을 주입할 수 있다. pool 공유가 테이블 접근권 공유를 뜻하지는 않는다. DB handle은 module의 비공개 adapter 안에 두고 application에 노출하지 않는다. SQLite 단일 writer/접속 설정을 유지한다. trading transaction 안에서 다른 module의 Repository나 DB 호출을 하지 않는다.

portfolio의 원장·가격·환율·분할 정보는 **하나의 SQLite 읽기 transaction에서 얻은 관측 묶음**으로 받는다. owner별 공개 조회는 15.12의 관측 참조로 같은 transaction에 결합하며 각각 별도의 transaction을 열지 않는다. 동일한 시각 문자열이나 각자 최신 revision만으로 일관성을 대체하지 않는다. 결과에는 기존 wire 계약의 ledger revision·as_of·상태를 보존하고 내부 입력 묶음에는 각 입력 ID/버전·계산 version을 기록한다. 외부 응답에 새 필드를 임의로 추가하지 않는다. 가격 조회 실패 시 이전 관측의 일부와 새 revision을 섞지 않는다.

Flutter는 기존 7 feature 설계를 유지한다. domain/application은 Flutter/HTTP에 의존하지 않고 data가 endpoint codec을 구현하며 app이 navigation/session을 연결한다. 다른 feature의 data/presentation import는 금지한다. Python research는 하나의 module이며 기존 facade/CLI는 호환 진입점으로 제한한다. 언어 사이는 계약 파일과 artifact로 연결하며 내부 모델을 공통 라이브러리에 밀어 넣지 않는다.

### 15.6 실패·권한·트랜잭션 계약

| 작업 | 한 owner 안에서 commit하는 상태 | 중간 실패·재시도 |
| --- | --- | --- |
| ledger ApplyImport | event·revision·receipt | 같은 key/입력은 첫 receipt를 재사용하고 입력이 다르면 기존 충돌 오류. 부분 event를 남기지 않음 |
| trading admission | signal·intent·authorization·연속 RISK_APPROVED/SUBMIT_DISPATCHED pair·local ACK | 하나라도 실패하면 모두 rollback. signal 재송신으로 authorization을 늘리지 않음 |
| trading fill | bar proof·capacity·fill·paper 회계·replay revision | 같은 source fill의 중복 효과 없음. 용량 초과/증거 불일치는 모두 rollback |
| policy | decision·모든 대상 halt·selection rollback·필요한 lease 갱신 | 일부 halt만 성공하지 않음. runner/fence/selection 경합을 같은 transaction에서 다시 검사 |
| broker submit | preflight 후 권한·예약·dispatch commit → 외부 submit 한 번 → outcome commit | timeout/commit 불명은 unknown. 무조건 재송신 금지. 기존 known-order reconciliation 유지 |
| promotion | claim commit → immutable 입력으로 DB transaction 밖 비교 → cutoff/selection 재검사 후 finish | crash는 PENDING, 경합은 기존 stale 결과. 제한된 cleanup의 취소/종료 계약 유지 |

조회 snapshot은 업무 실행 권한의 증명이 아니다. 가격/연구 입력을 고정해도 trading은 dispatch 시 자신의 authority·fence·기한·kill switch를 같은 transaction에서 판단한다. IdP/Redis/MQ 도입으로 이 판단을 대체하지 않는다. 시장 외부 조회와 저장 사이에는 listing 재확인을 유지한다. 새 role/tenant/자동매매 허용은 추가하지 않는다.

여러 instance는 composition에서 ID·설정·account scope·client pool을 구분한다. 현재 global selection/runner/policy와 단일 writer를 instance 수만큼 복제하지 않고 같은 authority DB를 참조한다. 여러 DB에 동시에 commit하는 UoW는 공개하지 않는다. DB/cache/MQ/IdP 후보와 이전 조건은 기존 `scalability-and-platform.md`를 따르며 미구현 profile을 설정 선택지에 노출하지 않는다.

### 15.7 확장 계약의 현재와 미래

지금 필요한 확장은 개발자가 추가하는 가격 공급자·broker·입력 형식·연구 전략이다. consumer가 요구하는 작은 port와 composition 등록으로 연결하고 내장 adapter도 같은 contract suite로 검사한다. 범용 등록 기반은 추가하지 않으며 각 port의 실제 입출력·기한·실패/unknown·멱등성 계약을 개별 정의한다.

사용자가 지정한 연구 설정과 artifact는 데이터로 검증하고 실행 코드로 import/eval하지 않는다. 서명이나 설치가 거래 권한을 부여하지 않는다. 사용자 코드 확장이 제품 요구가 되는 경우에만 다음을 도입 전 gate로 적용한다.

- host가 API/schema version, content hash, 허용 action, tenant/account/mode와 실효 여부를 관리한다. 설치·활성화·실행 허가·매매 mandate는 별도 상태다.
- credentials·DB·임의 network/filesystem을 전달하지 않고 broker 조작을 extension API로 공개하지 않는다. 제안 결과를 host가 다시 검증한 뒤 통상의 trading authority로 전달한다.
- CPU·메모리·출력 bytes·동시 실행·시간·외부 비용의 상한, cancel→종료 대기→강제 중단과 소유 자원 회수를 host가 강제한다. 기한의 값은 실측과 운영 합의 전에는 정하지 않는다.
- 제삼자 코드를 in-process Go plugin/Python import로 격리할 수 있다고 보지 않는다. OS sandbox/별도 process 등으로 실제 제한하고 escape·timeout·crash·version 불일치·철회 검사가 통과하기 전에는 허용하지 않는다. 내장 확장도 같은 contract suite를 통과해야 한다.

### 15.8 자동 검사와 TDD 수용 조건

검사 추가 위치는 기존 Go `internal/architecture/dependencies_test.go`, Python `tests/test_boundary.py`, Flutter `test/architecture_test.dart`(checkpoint와 비교 후 신설/통합)다. 존재하지 않는 검사를 실행했다고 기록하지 않는다.

| 검사 | 구현할 판정 | 의도적으로 위반하는 실패 fixture |
| --- | --- | --- |
| module import graph | 모든 production package의 import로 module graph를 생성해 cycle 거부. nested internal은 Go compiler도 거부 | A→B→A, 다른 module private import, alias/dot import 우회 |
| layer graph | domain은 순수 계산, application은 domain/consumer port만 사용. SQL/HTTP/fs/process/환경/clock 생성은 adapter, 생성 배선은 composition에 제한 | application→sqlite, domain→clock.Now, HTTP DTO를 domain에서 반환 |
| public contract | 업무 공개 함수 인자/결과에서 비공개 row/DB/HTTP/provider 타입 누출 거부. `compose.go`의 construction 설정과 Compose만 DB 주입 예외이며 application/domain에서 호출 금지. scope/iterator가 callback 밖으로 탈출하지 않는 계약 검사 | DB handle을 가진 DTO, 내부 package 타입 alias 재export |
| table ownership | 단일 owner registry에서 schema inventory와 SQL access 기대값 생성. SQL은 private owner adapter에만 허용. 고정 SQL/승인된 상수를 해석하고 미해결 동적 table 지정은 실패 | 다른 owner SELECT/JOIN/INSERT, 미등록 table/view/trigger, 동적 table 우회 |
| maintenance 예외 | migration/restore/물리 backup을 path+작업으로 제한하고 일반 command에서 import/실행 금지. 정합성 검사는 owner 계약으로 집계. 시스템 카탈로그 읽기는 별도 제한 규칙 적용 | maintenance 범용 Query를 application에서 호출, 일반 갱신을 restore로 위장 |
| Dart/Python | import/export/part/conditional import와 상대/절대 import를 정규화해 production 전체 조사 | feature private 참조, application→file adapter, facade 역방향 import |

SQL 정적 검사만으로 동적 SQL이나 SQLite 공유 pool의 실행 시 접근 통제를 완전히 증명했다고 말하지 않는다. 먼저 SQL 구성 방식을 유한한 literal/const로 제한하고 해석할 수 없으면 거부한다. 금융 계산·선택·복구 증명을 SQL gate로 옮기지 않는다. 이 검사를 위해 공통 SQL broker나 생성 DSL을 도입하지 않는다.

테스트 주도 전환 단위는 기존 업무 작업 하나다. ①현행 입출력/금액/hash/error/순서의 특성화 확인 → ②금지 import/소유권 검사의 RED 확인 → ③모든 호출자와 구현을 함께 이동 → ④집중 계약/통합 검사 → ⑤기존 구현과 임시 alias 삭제. 새 동작은 기대 결과의 RED를 먼저 확인한다. 폴더 이동을 테스트 대신 사용하지 않는다.

| 시나리오 ID | 보존할 결과 | 검증 계층과 기존 근거 |
| --- | --- | --- |
| MOD-01 | import 재전송은 receipt를 재사용하고 입력 불일치는 거부 | ledger API/SQLite 통합, `core_test.go`, `internal/adapters/sqlite/ledger_test.go` |
| MOD-02 | callback/error/commit/cancel/panic에서 부분 쓰기 없음 | 실제 SQLite 통합, 기존 scope 실패 검사 재사용 |
| MOD-03 | admission/runner heartbeat/policy 경합에서 하나의 유효 authority와 연속 pair 유지 | 실제 DB·독립 접속·barrier 경합, `paper_execution_authority_test.go`, `paper_runner_lease_test.go` 이식 |
| MOD-04 | dispatch 전 crash는 미송신, dispatch 후 unknown은 재송신하지 않음 | 실제 DB+제어 transport, `kiwoom_order_transport_test.go` 거부/unknown/unauthorized 검사 |
| MOD-05 | policy 실패로 halt/rollback 중 하나만 남지 않음 | 실제 DB 중간 실패 주입, `paper_performance_policy_test.go` 증명 보존 |
| MOD-06 | promotion 중 selection이 바뀌면 채택하지 않음 | 실제 DB+비교 중 barrier, `paper_promotion_test.go` cutoff/경합 검사 |
| MOD-07 | wire/null/배열/필드 순/hash/FIFO 잔여/backup 호환 보존 | contracts fixture+Go/Python 공유 계약+실제 DB restore 검사 |
| MOD-08 | route를 넘어도 미확정 apply는 처음 포착한 입력/key 유지 | Flutter controller/widget, 기존 import_apply 관련 검사를 이식 |
| MOD-09 | 공개 정보/capture/research가 자동으로 등록 가능 후보나 실행 권한이 되지 않음 | Python artifact/Go 수락 계약, capture/research/promotion 검사 |
| MOD-10 | 금지 의존/다른 owner SQL/미지정 table을 CI가 거부 | 위 표의 negative fixture. 일반 build 성공과 별도로 실패 확인 |

이는 기존 규칙에서 도출한 검증 계획이다. 새 매매 정책의 사용자 승인 기록이 아니다. 각 이식 시작 시 시나리오 ID를 실제 test 함수와 연결한다. 미구현 경합 검사나 미실행 검사를 합격으로 처리하지 않는다.

### 15.9 안정 상태를 유지하는 구현 순서와 완료 조건

1. **경계 고정**: owner registry·현행 table inventory·공개 DTO·의존 graph를 고정한다. 기존 root 예외는 path+symbol+이유를 열거하고 증가를 금지한다. migration 이력과 wire bytes는 보존한다.
2. **원자적으로 완결되는 작은 module부터**: watchlist를 독립 이식하고, ledger의 공개 자격/분할 조회 → marketdata → portfolio 순서로 옮긴다. 세 module의 공통 관측 배선은 15.12의 동일 transaction 계약을 먼저 확정한다. 비공개 구현·공개 계약·모든 caller·fixture를 함께 옮긴다. 시작 파일은 `watchlist*.go`, `owner_{fx,price}_series*.go`, `fx_observation.go`, `security_price_observation.go`, `instrument_listing*.go`, `market_data.go`, 기존 ledger 분리 파일, `portfolio_*.go`와 대응 HTTP/CLI/test다. 각 단위에서 실재 파일 전체를 열거한 수정 허용 목록을 확정한 뒤 편집한다.
3. **trading은 하나의 owner로 내부 이식**: order/authority/reservation→paper accounting/fill→strategy registry/selection/policy→promotion/runner 순서다. `order_*.go`, `execution_authority.go`, `paper_*.go`, `strategy_*.go`, `kiwoom_order_{transport,reconciliation}.go`와 caller/test를 추적한다. 이 순서는 trading 내부의 정리 순서이며 각각을 독립 이동 commit으로 강제하지 않는다. 같은 transaction을 호출하는 전체 closure를 한 이동 단위로 옮긴다. root policy/paper가 private journal에 의존한다면 trading transaction kernel과 그 전체 호출 closure를 먼저 함께 옮기고 내부에서 순차 정리한다. 외부 `*sql.Tx`/callback bridge를 임시 공개하지 않는다. 매 단계는 빌드 가능해야 하며 기존 transaction 일부만 다른 module로 옮기지 않는다.
4. **client/research/maintenance 통합**: Flutter checkpoint의 두 실패와 helper 소유권을 수정하고 7 feature로 이식한다. Python의 기존 public facade/4 CLI/byte 호환을 유지한다. migration/restore/proof를 owner 계약에 연결해 root Service와 공유 DB 직접 의존을 해소한다. 목표 `internal/modules/<owner>/api.go`와 비공개 domain/application/adapter로 옮기면서 기존 경로를 같은 단위에서 삭제한다.
5. **최종 gate**: 최종 diff에서 `make check` 한 번, 이후 `make smoke`, 필요한 Podman runtime/Flutter release 검사와 독립 리뷰를 수행하고 작업 전용 자원을 청소한다. CI·배포·credential이 필요한 broker 검증은 별도 상태다. 이번 설계에서는 실행하지 않는다.

각 단위의 종료 조건은 모든 caller 이식·기존 경로 삭제·계약/소유권/의존 검사·해당 통합 검사의 통과다. 새 adapter는 실제 구현과 공통 contract suite가 갖춰진 뒤에만 지원 목록에 추가한다. Go/Rust 전환, DB/MQ 도입, auth/live/확장 실행 호스트를 이식의 선행 조건으로 삼지 않는다.

부하·이용 건수·지연 SLO·비용의 현재 실측은 이번에 수집하지 않았다. 처리량을 추측해 cache/queue/DB를 추가하지 않는다. replay와 전수 proof의 비용은 기존 상한을 보존하고 측정 후 별도 projection을 검토한다. 설계 완료는 실제 운영·수익성 검증 완료를 뜻하지 않는다.


### 15.10 설계 근거와 구현 착수 조건

작성자는 Git의 `YouSangSon`, 설계 확인 기준은 `f6a6075`다. 이번 요청의 여섯 항목은 각각 15.4(업무·소유권), 15.5(의존·배치), 15.8(자동 검사), 15.6–7(확장·실행 계약), 15.8–9(TDD·단계별 검증), 15.7(조건부 사용자 확장)에 연결했다. 기존 설계와 다른 부분은 15.1에 대체 범위를 표시했다. section별 신규 인터뷰는 기존 목표 합의를 반복하지 말라는 사용자 기준에 따라 생략했다.

이 문서는 아키텍처 설계안이며, 아직 실행 단위별 모든 파일의 이동 목록을 고정한 수정 허용 목록은 아니다. 각 단위에서 root의 실제 호출자·테스트·fixture를 열거하고 공개 DTO 시그니처를 확정하는 일이 구현 시작 gate다. architecture 초안을 구현 준비 완료나 자동 검사 완료로 해석하지 않는다.


#### 경계 예외를 좁히는 구체 계약

- `broker_snapshot.go:appendKiwoomSnapshot`은 snapshot 저장과 같은 transaction에서 원장 수량을 읽고 reconciliation을 기록한다. 두 테이블을 ledger의 내부 reconciliation 영역에 귀속시켜 이 원자성을 유지한다. broker 조회는 consumer port로 transaction 밖에서 수행하고, 공개 RecordBrokerSnapshot은 검증된 관측값을 받아 원장 revision과 함께 저장·대조한다. raw Kiwoom wire 타입은 private provider adapter에서 해석한다. trading은 이 테이블을 직접 읽지 않는다.
- `schema_migrations`는 업무 데이터가 아닌 저장소 제어 metadata다. maintenance만 수정하며 각 owner의 SQLite adapter는 기동·proof에 필요한 version/schema 확인을 읽기 전용으로 수행할 수 있다. owner registry에 이 제어 테이블과 15.12의 제한된 SQLite 시스템 카탈로그 읽기 예외를 별도 분류한다. metadata가 일치한다는 사실만으로 업무 proof를 통과시키지 않는다. maintenance의 public API를 역으로 호출할 필요는 없다.
- 현행 `createBackup`의 `VACUUM INTO`는 storage 전용 maintenance adapter가 수행한다. 현행 source owner proof → VACUUM INTO → candidate proof → source/candidate 비교의 순서를 보존하며 불일치하면 실패시킨다. 생성된 candidate DB를 변경 불가능한 검증 대상으로 두고, owner별 공개 VerifyBackup이 각자의 상태와 cross-record 증거를 검사한다. 별도 live DB들을 순서대로 읽고 같은 시점 백업이라고 주장하지 않는다. restore는 staging DB에 수행하고 모든 owner proof·기존 manifest/hash 비교가 끝난 뒤 기존 배타적 복구 절차로 전환한다. 업무 테이블을 직접 읽는 범용 maintenance Query API는 노출하지 않는다.
- 공개 DTO는 owner의 `contracts` package가 소유한다. private application이 contracts를 import해 공개 command/result를 사용하고 domain의 비공개 값으로 변환한다. module root는 private application 생성자를 연결해 contracts의 작은 업무 capability를 반환한다. 다른 module과 inbound adapter는 contracts만 업무 의존성으로 사용한다. 함수마다 interface를 만들거나 root에 같은 메서드의 전달 wrapper를 반복하지 않는다. Serialize/Deserialize나 type alias로 private 타입을 재공개하는 우회는 금지한다. `compose.go`는 설정·pool을 받는 construction 전용 예외이고 업무 API와 별도로 검사한다.
- composition은 종료 시 신규 요청을 막고 owner별 worker를 취소한 뒤 종료를 기다리며 마지막에 pool/client를 닫는다. worker/child process/stream은 생성 owner가 cancel·join·강제 종료와 남은 자원 회수를 책임진다. 공유 pool을 빌린 module은 pool을 닫지 않는다. scope는 callback 안에서만 유효하며 외부 호출을 transaction 안에 넣지 않는다.


### 15.11 이번 설계의 검증 범위

2026-11-09에 새 대화 맥락의 Astra 독립 reviewer가 EGM Design Critic 기준으로 최신 15절과 기존 3·5·14절, ledger port/adapter, broker reconciliation, policy/promotion, backup proof 원본을 대조했다. 지적된 잔고 대조 테이블 누락, construction 타입 예외, schema metadata 읽기, 공개 contracts 위치, transaction 전체 호출 묶음 이식, source/candidate backup 동등 비교를 보완했다. 당시 보완본 재검토에서는 남은 Blocker/Major를 발견하지 못했다. 이후 재검토에서 동일 관측 보존, 원장 종목/분할 조회 계약, 시스템 카탈로그 예외의 공백을 추가로 확인했으며 이를 15.12에서 보완한다. 당시 판정을 현재의 완전성 보장으로 사용하지 않는다. 이는 설계 검토 결과이며 실행 검증이 아니다. 대화 맥락 분리는 `fork_turns=none`으로 설정했으며 host 지침 파일까지 제외된다고 주장하지 않는다.

문서 상대 파일 링크·코드 fence·한국어 문체와 `git diff --check`를 검사했다. migration 001–027에서 교체용 이름을 제외한 현재 테이블 이름 33개가 소유권 표/예외 설명에 포함됐는지 기계적으로 대조했다. 이 이름 대조가 SQL 호출 경로 전체의 소유권 검증을 뜻하지는 않는다. 이번 변경은 이 설계 문서 하나뿐이며 production 코드·계약·schema·의존성·설정을 변경하지 않았다. 테스트·빌드·배포와 새 임시 자원 생성은 수행하지 않았다.


### 15.12 재검토 보완: 동일 관측과 원장 의존의 보존

이 절은 2026-11-09의 두 번째 검토에서 발견한 세 공백을 수정한다. 15.5의 별도 순차 snapshot 조회안은 폐기한다. module 소유권·공개 계약은 유지하되, **하나의 DB에 있는 기존 일관성 경계를 유지하는 전용 저장소 배선**을 다음과 같이 한정한다. 새 DB·전역 이벤트 버스·일반 분산 transaction은 도입하지 않는다.

#### A. 관측 묶음의 구성과 수명

`current_portfolio_valuation.go:currentPortfolioValuation`은 같은 `*sql.Tx`에서 FX·가격·원장 proof를 얻고 입력의 tx/cutoff 동일성까지 검사한다. holding/XIRR/TWR/benchmark 경로도 같은 입력 구성에 의존한다. 이 보장은 동작 보존 refactor의 필수 계약이다.

목표 저장소 배선은 `internal/storage/observation`의 SQLite session 구현과 `internal/storage/contracts`의 불투명 `ObservationRef` 값으로 구성한다. `internal/storage/contracts`는 IO/DB 타입이 없는 순수 계약값만 갖고 domain에서는 사용하지 않는다. session 구현과 resolver는 바깥 인프라다. 이 패키지는 업무 테이블 SQL을 소유하지 않는다. composition에서 module별 private SQLite adapter에 같은 session registry에 연결된 제한별 resolver를 주입한다. resolver는 실제 transaction에 묶인 제한된 handle을 private adapter에만 제공하며 DB/Tx를 domain/application/업무 contracts에 노출하지 않는다. 읽기 resolver와 marketdata owner-price 전용 쓰기 resolver는 composition이 서로 다른 타입과 인스턴스로 주입한다. caller가 owner/purpose 문자열을 바꿔 쓰기 resolver를 선택하는 API는 없다. construction 전용 DB 주입 예외와 동일하게 검사한다.

- portfolio application의 입력 port는 `ReadValuationInputs(ctx, query) → ValuationInputs/error`다. 구현은 composition에 연결한 관측 coordinator adapter다. 이 adapter는 전용 읽기 transaction을 한 번 시작하고 server cutoff를 한 번 정한 뒤 해당 transaction에 묶인 ObservationRef를 발급한다.
- coordinator는 ledger와 marketdata의 **공개 읽기 계약**에 같은 참조를 명시적으로 전달한다. ledger가 원장 proof·잔고·종목 자격·분할 정보를 읽고 marketdata가 가격/FX/benchmark 버전을 읽는다. 각 owner adapter가 읽기 resolver로 동일 transaction을 사용하고 자기 테이블만 조회한다. domain에는 ref를 전달하지 않는다.
- `ObservationRef`는 revision/시간 문자열이 아니라 생성 instance·용도·scope·활성 session에 묶인 위조 불가능한 capability다. 외부 HTTP/CLI 입력으로 받을 수 없고 serialization·DB handle 추출·다른 요청 재사용을 허용하지 않는다. 로그에는 비밀이나 raw transaction을 남기지 않는다.
- owner의 관측 조회는 ref가 없거나 잘못됐거나 이미 종료됐거나 다른 resolver/DB에 속하면 실패한다. 별도 DB 조회나 새로운 transaction으로 fallback하지 않는다. module 내부에서 같은 관측 중 다시 일반 Repository를 호출하는 것도 금지한다. SQLite 단일 연결의 중첩 획득을 막는다.
- coordinator는 모든 입력/proof가 성공한 뒤 읽기 transaction을 정상 종료하고, 참조를 폐기한 다음 immutable 입력값만 application으로 반환한다. 오류·취소·panic은 rollback과 참조 폐기를 보장한다. scope·row·iterator·lazy loader·ObservationRef는 반환하는 입력 묶음에 포함하지 않는다. 외부 API/파일/연구 process 호출을 session 안에 넣지 않는다.
- 관심 종목의 `ReadWatchlistInputs` coordinator도 같은 방식을 사용한다. watchlist 상태/revision·가격/시각·필요한 ledger 자격 proof를 하나의 session과 cutoff로 읽고, 기존 watchlistQuote 규칙으로 missing/ambiguous/stale 결과를 계산한다. 현재 `watchlist.go:watchlist`의 동일 transaction 관측을 보존하며, watchlist module이 시장/원장 table을 직접 읽지 않는다. 이 경로에서 marketdata의 owner-price proof가 ledger를 조회할 때도 같은 읽기 참조를 사용한다.
- 기존 응답의 부족/오래됨/미검증 상태와 시각 의미를 유지한다. 모든 module의 최신값을 다시 받으며 부분 retry하지 않는다. 재시도하면 관측 묶음 전체를 새로 얻는다. 미래에 DB가 분리되면 이 adapter는 지원할 수 없으며, 단일 snapshot 보장을 낮추기 전에 별도의 버전 벡터/읽기 모델 계약과 정책 변경 검토가 필요하다.

이는 **명명된 평가 입력·관심 종목 quote 조회, 다음 사용자 가격 등록과 기존 owner-price proof 검증만을 위한 인프라 배선**이다. 업무 application이 임의 owner의 SQL을 실행하거나 callback을 조립하는 범용 UoW가 아니다. trading의 write transaction 밖에 있는 module Repository 호출을 끼워 넣는 예외로 확대하지 않는다.

#### B. 원장 종목 자격·분할 규칙과 사용자 가격 등록

`owner_price_valuation.go:admittedOwnerPriceIdentities`의 원장 replay/proof 및 canonical instrument ID 검증은 ledger가 소유한다. 공개 `AdmittedPriceIdentities(ctx, observation)`는 검증한 `(instrument_id, currency)`와 ledger revision을 반환하며 원본 event/receipt의 불필요한 필드를 노출하지 않는다. 모듈 내부 table 조회와 replay는 ledger에 남긴다.

`readValuationPriceInputs`의 SPLIT 조회는 ledger 공개 `ValuationFacts(ctx, observation)`로 옮긴다. 결과는 해당 관측의 원장 snapshot·시각 검증 정보·종목/통화별 마지막 분할 시각을 포함한다. 시장 관측의 해석·출처·canonical 가격은 marketdata가, 가격과 분할의 시간 관계에 따른 평가 가능성은 portfolio domain이 소유한다. 현재의 `afterLastSplit` 동작과 거부 결과를 그대로 이식한다. `marketdata → ledger`만 허용하고 ledger가 가격 공급자를 호출하는 역방향 의존은 만들지 않는다.

사용자 가격 등록은 기존 `owner_price_series.go`에서 자격 검증·기존 가격 출처 충돌·버전 멱등성·삽입을 **같은 쓰기 transaction**에서 수행한다. 자격만 먼저 조회한 뒤 별도 transaction에서 저장하도록 바꾸지 않는다.

- 명명된 `ImportOwnerPrice` storage coordinator adapter가 transaction 하나와 용도가 제한된 관측 참조를 만든다. 참조를 받는 ledger 공개 조회는 읽기 전용이다. marketdata는 자기 transaction-local adapter로 기존 가격 출처·schema·버전을 검증하고 자기 테이블만 쓴다.
- 이 작업은 marketdata application이 소비하는 전용 atomic import port로 연결한다. coordinator는 ledger 공개 자격 조회와 marketdata 공개 scoped-import 계약만 조합한다. scoped-import는 resolver가 허용한 **marketdata owner-price import 용도**일 때만 쓰기 handle을 사용할 수 있다. ledger와 나머지 owner에는 읽기 resolver만 주입한다. 이 resolver는 원본 `*sql.Tx`를 반환하지 않는 비공개 wrapper를 제공한다. Exec/Commit/Rollback/RawTx 또는 원본 transaction으로의 type assertion 경로를 노출하지 않는다. Query 계열만 제공해도 쓰기 SQL 실행이 가능하므로, wrapper는 registry에 고정한 owner별 읽기 전용 statement 전체와 인자 계약만 허용하고 INSERT/UPDATE/DELETE/DDL/미승인 PRAGMA·쓰기 함수 등을 거부한다. 쓰기 resolver는 marketdata의 해당 작업 adapter에만 주입하며 owner-price 관련 허용 statement 밖의 실행을 거부한다. public 범용 transaction/callback/Query/Exec/commit 메서드는 제공하지 않는다.
- 동일 source version/동일 hash는 기존 receipt, 불일치는 기존 충돌 오류로 처리한다. 자격/proof/출처 혼합/쓰기/commit 실패는 전체 실패다. 결과는 commit 확인 후에만 반환한다. 참조는 성공·실패 모두 폐기하고 session 밖으로 내보내지 않는다.
- 현재 candidate backup DB의 owner-price proof도 원장 자격에 의존한다. marketdata의 공개 VerifyBackup이 ledger의 공개 자격 조회를 같은 immutable candidate 관측으로 소비하게 한다. 경로 문자열만 받고 다른 live DB로 연결하는 fallback은 금지한다. maintenance는 proof를 집계할 뿐 원장 table SQL을 대신 소유하지 않는다.

외부 ImportOwnerPrice → marketdata application의 atomic import port → coordinator → 별도 scoped validation/persist use case 순서다. scoped 진입은 같은 ImportOwnerPrice 또는 atomic import port를 다시 호출하지 않는다. outer ImportOwnerPrice 유스케이스는 atomic import port만 호출한다. 별도 scoped handler는 transaction 시작기·일반 Repository·atomic import port를 의존하지 않으며 주입된 session-bound store만 사용한다. Compose는 scoped handler 생성 → coordinator에 scoped capability 주입 → outer use case 생성 순서로 연결한다. 동일 Service 인스턴스를 서로 주입하는 객체 cycle은 금지하고 정적 호출/배선 검사와 재진입 실패 fixture를 둔다. 실제 업무 규칙은 marketdata domain/application에 두고 coordinator에는 신규 자격·가격 규칙을 복제하지 않는다.

이 제한된 read-ledger/write-marketdata 결합은 기존 코드에서 확인한 동작을 보존하기 위한 예외다. 여러 업무 module에 동시에 쓰는 분산 transaction 계약이 아니며, 이 결합을 제거하기 위한 증명 가능한 대안이 나오기 전까지 유지한다. 추후 ledger 의미가 수정·삭제 가능한 자격으로 바뀌어도 별도 사전 조회로 임의 약화하지 않는다.

#### C. SQLite 시스템 카탈로그 읽기 예외

owner registry는 **업무 테이블**, **공유 제어 metadata**, **SQLite 시스템 카탈로그**를 구분한다. `sqlite_master`·`sqlite_temp_master`는 새로운 업무 owner를 배정할 테이블이 아니다. 현재 watchlist/owner-price 등의 물리 schema 검증에 필요한 읽기만 허용한다.

- owner의 private SQLite schema/proof adapter는 자신의 테이블·인덱스·trigger 정의와 임시 shadow object 존재 여부를 확인하는 고정 카탈로그 SELECT만 사용할 수 있다. 이름 인자는 registry에 등록된 owner 객체에서만 얻는다. 임의 table명과 전체 SQL text를 반환하는 공용 API는 금지한다.
- 자기 객체와 충돌하는 temp shadow를 탐지하는 조회는 허용하되 다른 owner의 데이터를 읽는 SQL로 이어지지 않게 한다. 객체 개수/정의/hash 대조를 생략하지 않는다. 전역 schema inventory는 maintenance의 기동/복구 전용 adapter가 관리한다.
- `schema_migrations`는 기존처럼 maintenance만 쓰고 owner는 version/schema 확인에 한해 읽는다. 카탈로그 UPDATE/DELETE/INSERT, `writable_schema` 활성화, 미승인 PRAGMA/ATTACH, 해석 불가능한 동적 SQL은 허용하지 않는다. migration의 정해진 DDL 예외를 일반 command에 재사용하지 않는다.
- 카탈로그 SQL도 경로·연산·대상 객체·용도로 제한한 검사의 대상이다. 단순히 시스템 테이블 이름이 포함됐다는 이유로 쿼리 전체를 allow하지 않는다.

#### D. 추가 수용 시나리오와 구현 순서

| 시나리오 | 기대 결과 | 검사 계층 |
| --- | --- | --- |
| MOD-11 평가 중 원장·가격·FX 갱신 경합 | 현재/holding/XIRR/TWR/benchmark가 같은 DB snapshot과 cutoff의 입력만 사용. 서로 다른 session/cutoff 결합은 거부 | 실제 SQLite 독립 접속+barrier, 기존 tx 동일성 검사 이식 |
| MOD-12 사용자 가격 자격·등록 | 원장 자격 proof와 시장 출처/버전 검증·삽입은 한 transaction. 중간 실패/취소/commit 실패는 신규 버전 없음 | 실제 SQLite+실패 주입. 기존 owner-price 멱등성/미등록 종목/출처 혼합 검사 유지 |
| MOD-13 관측 참조 수명·권한 | 종료/다른 instance/다른 용도/위조 ref 거부. 읽기 wrapper의 쓰기 SQL/원본 Tx 반환·쓰기 resolver 주입·별도 DB fallback 불가 | session adapter 계약+실제 DB, panic/cancel 종료 후 연결 재사용 검사 |
| MOD-14 schema proof 예외 | 자기 카탈로그 정의/개수/임시 shadow 검사는 허용. 다른 owner/동적 객체/카탈로그 쓰기/미승인 PRAGMA는 거부 | architecture negative fixture+실제 schema 변조/복구 검사 |
| MOD-15 candidate owner-price proof | 같은 candidate의 원장 자격·가격을 검증하고 source/candidate 동등성 유지. live 데이터 혼합 없음 | 실제 backup/restore 통합 |
| MOD-16 관심 종목 관측 경합 | watchlist revision·quote·owner-price 자격 proof가 같은 session/cutoff를 사용하며 목록 갱신 중 이전/신규 입력을 섞지 않음 | 실제 SQLite 독립 접속+barrier, 기존 watchlist quote/CAS 계약 |

15.9의 순서를 보정한다. 먼저 관측 session과 owner별 공개 읽기 계약의 RED/수명 검사를 정의한다. ledger 자격/분할 조회를 추출한 다음, marketdata의 관측 조회·atomic owner-price import를 함께 이식하고 portfolio 조회를 이전한다. SQL/transaction을 public callback으로 임시 노출해 중간 단계를 통과시키지 않는다. 각 단위에서 실제 caller·test·fixture의 수정 허용 목록을 확정하고 동작이 보존된 완전한 묶음으로 이동한다. 이 절은 설계 변경이며 검사 구현·실행 완료 기록은 아니다.


관측 연결의 대안도 대조했다. process-local lock만으로는 다른 process의 writer를 막지 못한다. 같은 cutoff만 넘기거나 각 module의 최신 version을 순서대로 읽는 방식은 기존 한 transaction의 관측을 보장하지 않는다. 전역 commit version과 모든 writer의 version stamping을 새로 도입하는 안은 schema·쓰기 경로 전체를 바꾸므로 이번 보존 refactor에서 채택하지 않는다. 기존 SQLite transaction을 유지하는 좁은 인프라 session이 현재 확인한 계약을 가장 직접적으로 보존한다.


resolver의 쓰기 제한은 동적으로 해석한 SQL 전체에 대한 보안 증명으로 포장하지 않는다. 허용 statement의 완전 일치와 금지 우회 경로에 대한 정적 검사·실행 계약 검사를 함께 적용한다. 악성 in-process 코드가 reflection/unsafe로 메모리를 침범하는 상황의 격리 수단은 아니며, 그런 사용자 코드는 15.7의 별도 격리 gate를 따른다. 관측 session의 lifecycle 조율과 owner별 statement 제한은 storage 인프라에 두고 업무 규칙을 복제하지 않는다.


보완본은 독립 Astra reviewer가 최신 15.12 및 관련 15.5·15.8·예외 문장을 위 원본 코드와 다시 대조했다. 이 검토 범위에서 남은 Blocker/Major는 발견하지 못했다. 문서의 상대 파일 링크·fence·문체, 폐기한 순차 조회/단일 metadata 예외 문장의 제거, MOD-11–16 시나리오 연결과 `git diff --check`를 확인했다. 이번에는 설계 문서 하나만 수정했으며 runtime 검사·빌드·배포·새 캐시/리소스 생성은 수행하지 않았다. 실제 resolver의 권한 제한, transaction 경합·취소·복구는 구현 후 검증할 항목이다.
