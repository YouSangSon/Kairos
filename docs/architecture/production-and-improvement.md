# Kairos 프로덕션·제품·투자 개선 설계

| 항목 | 값 |
| --- | --- |
| 상태 | v1.0 · 설계 제안(Proposed) · 2026-11-16 |
| 작성자 | YouSangSon |
| 기준 소스 | `a2eba6fdb78754a9a67faa31f5b3bd2e1112320d` · PR #81 병합 이후 |
| 작업 | `production-improvement` · 티켓 미할당 |
| 구현·검증·배포 | 이 문서는 후속 설계다. 아래 신규 계약·테이블·화면·테스트는 아직 구현하거나 실행하지 않았다. |

## 한눈에 보기

- 자료를 모아 기회를 찾고, 근거와 위험을 설명하는 추천으로 연결한다.
- 추천 채택과 거래 승인을 분리하고, 승인된 범위에서 투자·리밸런싱을 실행한다.
- 비용과 손실을 포함해 결과를 복기하고, 검증을 통과한 개선 후보를 다음 운용에 반영한다.
- 먼저 개인용 단일 서비스의 신뢰성을 완성한다. 새 인프라는 측정한 필요가 있을 때 도입한다.
- 현재는 설계 단계다. 실제 계좌 연결·실전 활성화·위험 한도와 운영 목표의 숫자는 별도 확인이 필요하다.

## 요약

핵심 목표는 장기 자산 운용의 판단과 실행을 돕고, 같은 실수를 줄이는 것이다. 추천 수나 백테스트 최고 수익률만으로 개선을 판단하지 않는다. 비용을 차감한 성과, 손실과 자금 사용, 사용자가 확인하는 시간까지 함께 본다.

현재의 원장·평가·연구·모의 실행을 출발점으로 삼는다. 추천 정책과 불변 결정 기록은 신규 `advisory`(투자 제안·복기 담당)가 소유하고, 승인·자금 예약·주문·체결은 `trading`(거래 권위 담당)에 남긴다. 모듈은 업무 책임과 데이터 소유권으로 나누며, 공개 계약 외의 코드와 테이블에 접근하지 않는다.

개선은 두 흐름으로 운영한다. 제품 개선은 관측한 불편·오류·비용에서 변경안을 만들고 테스트·리뷰로 검증한다. 투자 개선은 복기에서 가설을 만들고 제한된 실험과 다음 미관측 구간의 모의 검증을 거친다. 어느 흐름도 스스로 실전 권한이나 운용 자금을 늘리지 않는다.

### 핵심 결정

| 결정 | 제안하는 구조 | 선택 이유 |
| --- | --- | --- |
| 업무 경계 | 기존 7개 Go owner에 advisory 1개 추가 | 실행하지 않는 추천·보류·복기도 독립적인 정책과 보존 책임을 가진다. |
| 금융 상태 | 거래 권위와 원장 반영은 같은 authority DB 안에서 조율 | 여러 종목의 외부 체결은 원자적이지 않다. 내부 상태까지 불필요하게 분산하지 않는다. |
| 자동 개선 | 연구·제안 자동화와 실행 권한을 분리 | 유리한 과거 결과나 새 프로그램 설치가 실전 승인이 되는 일을 막는다. |
| 운영 | 로컬 단일 Go writer + SQLite 유지 | 개인 운용에 필요한 복구·관측부터 완성하고 복잡도를 제한한다. |
| 확장 | 의미가 다른 저장·이벤트·작업 포트를 구분 | 여러 공급자와 DB·Kafka·RabbitMQ의 조합을 허용하되 실제 보장 차이를 숨기지 않는다. |

## 1. 목표·비목표와 현재 근거

### 1.1 목표와 완료의 의미

이 설계의 제품 범위는 **수집 → 판단·추천 → 승인된 투자·리밸런싱 → 성과·복기 → 개선 후보 검증**이다. KRW와 USD의 자산·현금·성과를 유지한다. 당장 지원하지 못하는 공급자 기능은 차단 사유로 표시하며 제품 목표에서 지우지 않는다.

| 축 | 원하는 결과 | 확인할 지표 |
| --- | --- | --- |
| 제품 기능·차별성 | 왜 제안했고 왜 보류했는지 과거 근거로 다시 설명할 수 있다. | 근거가 연결된 제안 비율, 누락 표시, 채택·보류 이유, 사용자의 검토 시간 |
| 효율 | 같은 정보를 반복 수집하거나 불필요하게 매매·실험하지 않는다. | 유효 자료당 호출·저장·연구 비용, 중복 비율, 회전율, 유효 결과당 소요 시간 |
| 투자 성과 | 비교 가능한 조건에서 비용 차감 성과와 감수한 위험을 함께 평가한다. | 순수익, 기준 전략 대비 성과, 최대 손실폭, 노출·집중도, 비용·불확실성 |
| 운영 신뢰성 | 재시작·응답 유실·복원에도 외부 주문을 함부로 반복하지 않는다. | 미확정 주문과 경과 시간, 대조 지연, 복원 검증, 예약·원장 정합성 |

이 지표의 목표 숫자는 아직 정하지 않았다. 수익 보장이나 손실 제거는 시스템의 보장이 아니다. 개인의 자금·허용 위험과 실제 데이터에 맞춰 평가 기준을 확정해야 한다.

이번 설계의 완료조건은 책임·공개 계약·실패와 재시도·자료 이동·단계별 검증을 문서로 연결하고, 중요한 모순을 독립 검토로 확인하는 것이다. 구현 완료, 로컬 테스트 통과, CI 통과, 배포 완료, 실제 운용 확인은 각각 별도 상태다.

### 1.2 범위 밖

이번 작업에서는 소스 구현, 실제 주문, credential 사용, 배포, 일시중지한 Goal 재개를 하지 않는다. Rust 전환, 다중 사용자 SaaS, 레버리지·공매도, 임의 프로그램을 실행하는 확장 호스트도 기본 범위에 추가하지 않는다. PostgreSQL·Redis·Kafka·RabbitMQ·Kubernetes를 설계의 완성도를 보여주기 위해 설치하지 않는다.

### 1.3 현재 구현과 남은 차이

아래는 기준 소스에서 확인한 사실이다. 기존 PR의 검사 통과는 해당 기반의 근거이며, 아래 신규 기능의 통과 근거가 아니다. 세부 근거 위치는 부록 A에 모았다.

| 영역 | 현재 확인한 상태 | 후속으로 필요한 것 |
| --- | --- | --- |
| 구조 | Go 7개 owner, private domain/application/adapters, composition 배선 | 신규 owner·계약도 같은 구조 검사에 편입 |
| 평가 | 원장·시장 입력을 한 읽기 transaction(함께 성공·실패하는 DB 처리)과 cutoff로 조회 | 불변 추천 근거와 당시 이용 가능 시각의 증거 |
| 연구 | Python 실험·walk-forward·holdout, Go research의 로컬 Mimir feed 읽기 | 실행 전 등록·모든 trial·가족 단위 검증 오염 추적·제한된 작업 실행 |
| 거래 | 등록 연구·선택·주문·예약·paper 회계·lease의 기존 원자적 경계 | 다종목 계획·개별 승인·mandate·실전 자산 검증과 원장 반영 |
| 승격 | `long_only_sma_crossover 1.0.0`만 기존 promotion 대상 | 다종목 리밸런싱에 별도 모델·계약·적합성 검증 |
| 서버 | 기본 loopback, HTTP timeout. serve 경로에 `Shutdown`/drain 연결 없음 | 신규 행동 차단, 진행 중 요청·runner 종료 대기, 복구 가능한 receipt |
| 운영 | 로컬 기반과 gate 문서 존재 | 실제 인증 mock 관찰, 배포·복구·알림·운영 측정 |

### 1.4 데이터·부하 가정과 보장 범위

관측한 기반은 개인용 local-first, SQLite 단일 writer, 별도 Python 프로세스다. 실사용 종목 수·계좌 수·tick 유입량·기사 크기·증가율·평균/최대 지연·장애 빈도·운영 비용은 측정하지 않았다. 기존 G6의 1 CPU/2 GiB는 선언한 예산이며 실측한 최소 사양이 아니다.

초기 수집은 공급자의 cursor를 따라 증분으로 읽고, 최근 구간은 정정 확인을 위해 겹쳐 읽는다. 조회는 기간·종목·계좌와 페이지 한도를 요구한다. 원문과 대형 연구 결과를 API 요청마다 메모리에 전부 적재하지 않는다. 실제 분포를 관측한 뒤 batch·pool·동시성 한도를 정한다.

| 하부 보장 | 애플리케이션이 직접 책임지는 것 |
| --- | --- |
| 같은 DB의 검증된 transaction | 서로 다른 owner의 허가된 접근, 중복 키·payload 충돌, 불변식과 복구 증명 |
| 파일 내용 hash | 출처의 정확성·사용권·발표 시각·미래 정보 혼입 여부. hash만으로 증명되지 않는다. |
| provider 조회·알림 | pagination 완결성, 누락·역순·정정·연결 단절, 계좌·환경 식별과 대조 |
| 프로세스 취소·lease 만료 | 자식 종료 확인, 남은 외부 요청, 이전 writer 차단과 안전한 인계 |
| 백업 | 백업 이후 외부 체결과 누락된 감사 기록. 복원만으로 알 수 없다. |

SQLite의 transaction 보장은 broker 호출까지 확장되지 않는다. WAL(변경 로그)도 여러 DB 파일을 하나의 금융 commit으로 묶지 않는다. 실제 driver·저널·동기화 설정과 저장장치 조건은 운영 profile에서 검증한다. [SQLite atomic commit](https://sqlite.org/atomiccommit.html), [WAL의 제한](https://sqlite.org/wal.html)

## 2. 사용자 흐름과 공개 계약

### 2.1 사용자가 먼저 보는 흐름

1. **현재 상태 확인:** 보유·현금·관심종목에서 원통화 금액과 가격·FX 기준 시각을 본다. 오래되거나 빠진 자료는 이유와 함께 표시한다. 이를 정상적인 현재 합계로 꾸미지 않는다.
2. **기회와 보류 비교:** 추천에서 출처·주장·반대 근거·비용·위험·만료를 확인한다. `유지`와 `판단 불가`도 정식 결과다. 사용자가 모든 추천을 주문으로 바꿔야 할 이유는 없다.
3. **계획 검토:** 목표와 현재 비중, 종목별 주문, 통화별 현금, 실행 순서·비용과 실패 시 남을 상태를 본다. 계획 채택, 한 번의 실행 승인, 지속 mandate(미리 승인한 범위 내 반복 실행 권한)는 다른 행동이다.
4. **실행 확인:** paper·shadow·live를 계속 구분한다. shadow는 실제 자료로 판단을 관찰하되 주문을 보내지 않는 모드다. 부분 체결·제출 불명은 완료가 아니다.
5. **복기와 개선:** 당시 예상·대안과 실제 결과를 비교하고 연구 가설로 연결한다. 개선 후보가 발견돼도 실전 전환·증액은 별도 승인한다.

### 2.2 제안하는 사용 예

다음 이름은 **신규 계약의 스케치**다. 현재 API나 구현된 타입이 아니다. 계층마다 같은 전달 interface를 반복하지 않는다. caller가 필요한 업무 결과를 받는 계약만 둔다.

```text
research.StartStudy(study_request) -> durable study_receipt
advisory.Generate(advice_request) -> immutable advice / insufficient_evidence
trading.PreviewRebalance(plan_request) -> registered executable_plan / rejected
trading.ApprovePlan(exact_plan_hash, expected_policy_revision) -> approval_receipt
trading.StartRebalance(plan_id, approval_or_mandate, command_id) -> execution_receipt
advisory.Review(advice_id, execution_refs, period) -> immutable review
```

추천 원문을 실행 권한으로 쓰지 않는다. 신규 계획의 서버 adapter는 추천의 정확한 바이트·schema·hash를 검증한 뒤 trading의 입력 값으로 변환한다. trading이 자신의 기준으로 검증하고 **별도의 실행 계획**을 등록한다. 두 기록은 추천 정본과 거래 계획 정본으로 역할이 다르다. 실행 가능한 수량·가격·순서·만료가 달라지면 새 계획 hash와 승인이 필요하다.

개별 승인은 **구체적인 주문 leg 목록**에 묶인다. 지속 자동 운용의 mandate는 승인한 배분/전략 정책 version, 허용 universe·목표/허용 편차·현금/노출·수량/가격·비용/회전율 한도, 실행 주기와 만료를 고정한다. 이 범위 안에서는 매번 새 자료로 계획을 생성하고 검증해 실행할 수 있다. 정책·한도 확대는 재승인이 필요하다. 정확한 숫자가 없는 mandate는 활성화하지 않는다.

### 2.3 최소 계약과 결과 형태

| owner / 계약 | 핵심 입력 | 저장한 결과·오류 |
| --- | --- | --- |
| research / StartStudy, Study | 요청 ID, 가설·데이터·탐색·비용 정책, 자원 예산 | 작업·trial 상태, artifact 참조, 누락·실패·검증 이력 |
| advisory / Generate, Advice | 요청 ID, scope, 추천 정책 revision, 근거 선택 조건 | ID/hash, 근거, 대안·비용·위험, 유효 시각, actionable/no_change/insufficient |
| trading / PreviewRebalance | 요청 ID, 계좌·mode, 추천 참조, 목표·제약·유효 시각 | 실행 계획 ID/hash, leg·필요 현금·제약, expected revision 또는 거부 이유 |
| trading / ApprovePlan, Revoke, Receipt | 명령 ID, 정확한 계획 hash·revision·주체, 승인 또는 철회 대상 | 승인·철회 commit receipt, 진행 중 또는 충돌 |
| trading / BlockEvidence | 제한된 owner/service 주체, 근거/추천 ID·hash·scope, 이유, 명령 ID | 실행 부적격 block의 commit receipt. 승인·권한 확대 기능은 없음 |
| trading / StartRebalance, Execution | 명령 ID, 계획 ID/hash, 개별 승인 또는 mandate 참조 | 실행 ID, leg별 주문·예약·체결·미해결 상태 |
| advisory / Review, ReviewResult | 요청 ID, 추천·실행 참조, 기간·비교 정책 | 근거 고정 보고서, 비교 가능성·누락·다음 가설 |

`scope`는 현재 개인 workspace와 명시적인 계좌·시장·통화·mode 범위를 뜻한다. 기존 global selection·sequence·watchlist·정책을 계좌별이나 tenant별로 바꿨다고 가정하지 않는다. 신규 계획 namespace는 legacy와 분리하고 §11의 migration gate를 거친다.

오류는 adapter가 HTTP/CLI 표현으로 변환한다. 신규 비동기 명령은 durable receipt를 만든 뒤에만 접수를 반환하고, 접수는 실행 완료가 아니다. 신규 HTTP 계약의 기본 구분은 잘못된 입력 400, 인증/권한 401/403, hash·revision·키 충돌 409, 의미상 부적합 422, 예산 초과 429, 의존성 미준비 503이다. 거래가 불명확한 상태는 저장된 200/202 조회 결과의 업무 상태로 표시한다. 기존 오류·wire 형식은 바꾸지 않는다.

### 2.4 재시도·중복·결과 조회

신규 업무 명령의 재시도 키는 `(workspace, owner, operation, provider_environment, account_or_study_scope, command_id)`다. 인증 주체의 권한을 먼저 확인한 뒤 해당 범위에서 키를 조회한다. 다른 주체의 receipt가 조회 키만으로 노출되지 않게 한다.

| 상황 | 정해진 결과 |
| --- | --- |
| 같은 키·같은 canonical 입력 | 최초 저장한 결과 또는 진행 상태 반환. 만료된 제안을 새 자료로 다시 계산하지 않음 |
| 같은 키·다른 입력 hash | 충돌. 기존 결과를 덮거나 새 외부 효과를 만들지 않음 |
| 동시 최초 요청 | DB unique 제약과 짧은 transaction으로 하나만 claim. 나머지는 같은 receipt 조회 |
| 응답/commit 결과 불명 | 같은 키로 조회·복구. 새 키를 만들어 주문을 다시 보내지 않음 |
| 작업 lease 만료 | 이전 attempt가 외부 효과를 냈는지와 소유 프로세스를 확인. 연구 작업과 주문 재시도 규칙은 다름 |

금융 명령·승인·주문·체결의 dedup 식별자와 최소 receipt는 연결된 감사·복구 기간 동안 제거하지 않는다. 실질적인 재사용 가능한 TTL을 금융 키에 두지 않는다. 원문 정리는 별도 보존 정책이다. 비금융 연구·수집 키는 최대 재전달·복구 기간보다 짧게 지우지 않으며 숫자는 예산·자료 보존 정책과 함께 확정한다.

요청 ID 중복 제거와 **업무상 한 번만 실행하는 규칙**은 다르다. `StartRebalance`는 같은 transaction에서 계획의 실행 생성과 개별 승인 소비를 기록한다. 계획당 execution 하나, execution/leg당 고정 intent 하나를 unique 제약으로 보장한다. 승인에는 `consumed_by_execution_id`를 묶으며 다른 plan/실행에 재사용하지 못한다. 같은 plan에 다른 command ID가 들어와도 기존 execution을 참조하고 주문을 늘리지 않는다. 이미 실행한 계획의 새 실행은 허용하지 않는다. 부분 실행의 재개도 같은 execution/leg/intent를 사용한다.

## 3. 업무 경계·데이터 소유권·의존 방향

### 3.1 owner별 정본

owner는 자신의 업무 규칙과 쓰기 권한을 소유하는 모듈이다. 같은 SQLite 파일을 쓴다는 이유로 다른 owner의 SQL·repository를 사용하지 않는다. 내부 형태는 기존 `contracts`, `internal/domain`, `internal/application`, `internal/adapters`, `compose.go`를 따른다.

| owner | 정본과 업무 규칙 | 공개하는 최소 정보 |
| --- | --- | --- |
| ledger | 거래 사실·현금·수량·정정·revision·FIFO | 평가 사실, 지정된 체결 반영·복구 계약 |
| marketdata | 가격·FX·상장·기업행사와 해당 원천 자료의 시각·정정·자격 | 검증한 관측과 lineage, 수집 상태·capability |
| portfolio | 평가·TWR·XIRR·기준 전략 비교 계산 | 같은 관측의 입력과 결과. 추천·주문 테이블은 소유하지 않음 |
| watchlist | 관심 목록과 revision | 탐색 대상. 거래 allowlist가 아님 |
| research | 현재 Mimir feed. 확장 후 연구 작업·전체 trial·artifact 등록/실행 이력 | version/hash가 있는 연구 증거와 상태. 거래 선택 권위는 없음 |
| advisory 신규 | 추천 정책 revision·근거 묶음·불변 추천·사용자 채택/보류·복기 | 제안과 보고서. 승인·예약·주문 credential 없음 |
| trading | 등록 연구·선택·실행 정책·승인·mandate·예약·주문·체결·실행 계획 | 명령 receipt, 실행 사실·성과 입력, 제한된 원자적 처리 계약 |
| maintenance | schema·백업·복원 조율 | owner별 증명 결과. 업무 규칙을 재정의하지 않음 |

Python은 research의 제한된 계산 worker다. 연구 artifact를 생성하되 운영 DB와 broker credential을 받지 않는다. Mimir의 공개 뉴스/연구 feed와 그 원문·주장 lineage는 research가 소유하고, 가격/FX 관측의 정본을 중복 소유하지 않는다. trading에 이미 등록한 연구 증거·선택을 Go research로 옮기지 않는다. 연구 계산 결과와 거래에서 수락한 증거는 hash로 연결하는 서로 다른 정본이다.

### 3.2 공개 의존과 캡슐화

기존 의존을 유지하고 `advisory → portfolio/contracts, research/contracts, trading/contracts`를 추가한다. trading에 대해서는 조회와 근거 차단 전용 계약만 사용하고, 승인·mandate 발급 계약을 주지 않는다. trading은 advisory를 import하지 않는다. portfolio는 trading을 import하지 않는다. `portfolio → ledger, marketdata`, `marketdata → ledger`, `trading → marketdata`, `watchlist → marketdata`를 유지한다. 원자적 조율은 composition의 제한된 연결 계약으로 해결한다.

추천에서 거래로의 전달은 versioned artifact/값이다. 거래 결과는 trading의 공개 조회를 통해 advisory가 읽는다. 왕복 업무 흐름이 import cycle을 요구하지 않는다. 초기 UI의 사람이 채택하거나 scheduler가 제안을 전달할 수 있지만, scheduler에는 권한 생성·한도 변경 계약을 주지 않는다.

domain은 순수한 업무 값과 판정만 사용한다. application은 유스케이스·작업 상태·취소를 조율한다. DB·파일·HTTP·프로세스·cache·MQ는 adapter가 구현한다. `Compose`는 검증된 설정으로 실제 인스턴스를 배선하며 업무 허용 여부를 판단하지 않는다. 모듈 사이에 `sql.DB`, `sql.Tx`, raw query executor, private 저장 모델을 노출하지 않는다.

### 3.3 자동 검사

기존 `services/core/internal/architecture/` 검사에 신규 owner·public contract·허용 의존을 등록한다. 다음 실제 위반 fixture도 함께 추가한다: 타 owner internal import, domain의 adapter import, 계약의 DB handle 노출, 순환 의존, 다른 owner 테이블 SQL, 허가되지 않은 transaction 목적, 탈출한 scope·rows. 허용 목록은 기능에 필요한 정확한 SQL과 역할만 늘린다.

Flutter와 Python의 기존 구조 검사도 새 feature·artifact 호출을 포함한다. Python에 Go DB 접근을 추가하거나 Flutter에 broker secret을 넣는 경로는 허용하지 않는다. 단순한 import 검사로 권한·transaction 안전까지 검증했다고 말하지 않는다. 해당 검증은 실제 DB·프로세스 테스트가 담당한다.

## 4. 지속 수집과 판단 근거

### 4.1 시간과 정정

시장 자료에는 경제 시각(`event_time`), 그 값이 외부에서 이용 가능해진 시각(`available_at`), provider 시각, 수집 시각(`fetched_at`), 내부 기록 시각(`recorded_at`)을 구분한다. 모르는 시각은 null/unknown으로 남긴다. 파일 생성 시각이나 같은 cutoff 문자열로 당시 이용 가능성을 대체하지 않는다.

원천 identity는 provider·environment·instrument·자료 종류·provider ID·version의 조합이다. 같은 identity/version/내용은 중복이고, 같은 identity/version의 다른 내용은 충돌 격리다. 공식 정정은 새 version과 supersedes 관계로 보관한다. 현재 projection은 교체할 수 있지만 과거 추천·실험이 참조한 값은 덮지 않는다.

수집 단위는 마지막으로 durable하게 수락한 cursor·watermark를 기록한다. DB에 결과와 cursor가 함께 commit되기 전에는 다음 위치로 진행하지 않는다. 파일 원문은 임시 파일 검증 → create-only 확정 → DB 참조 commit 순서다. 중간에 남은 orphan은 소유 manifest·미사용 여부·보존 기간을 확인해 회수한다. 실패를 cursor 전진으로 숨기지 않는다.

### 4.2 품질과 추천 자격

| 입력 | 확인할 내용 | 실패 시 동작 |
| --- | --- | --- |
| 가격·봉 | 시장 달력·timezone·봉 완료·조정 방식·정정·누락 | 표시 가능 자료와 연구/주문 가능 자료를 구분 |
| 종목 universe | 당시 상장·상장폐지·거래 가능·식별자 변경 | 현재 생존 종목만으로 과거 성과를 만들지 않음 |
| FX·기업행사 | 방향·시점·배당/분할·현금·세금·비용 기준 | 원통화는 유지, 불완전 환산·성과는 unavailable |
| 뉴스·공시·Mimir | 출처·게시/수정/수집 시각·사용권·주장과 근거 | 미확인 주장·LLM 요약을 사실·거래 신호로 승격하지 않음 |
| broker 자료 | 환경·계좌·페이지 완결·미체결/체결/정산 | 불완전 대조에서 자금이나 미제출을 확정하지 않음 |

현재 candle capture의 hash·provider 시각 보존만으로 봉 완료·adjustment·발표 시각·실제 인증을 증명하지 못한다. 해당 증거를 추가하지 않은 입력은 offline 분석에만 머문다. 신뢰할 수 없는 자료에도 `판단 불가`라는 사용자 결과를 제공한다.

`portfolio.InputsReader`의 같은 DB transaction/cutoff를 이용해 원장 revision·가격·FX·benchmark ID와 계산 version을 캡처한다. 외부 연구 파일과 별도로 읽은 trading 상태에는 각각 시각·revision을 기록한다. 하나의 동시 snapshot이라고 표기하지 않는다. 읽기 session이 끝나면 참조를 폐기하고 불변 값만 advisory에 넘긴다. 이는 과거 원장을 복원하는 일반 time-travel 기능이 아니다.

### 4.3 수집 효율·부하 제어

provider/environment/계좌/endpoint별 제한과 공급자 공유 제한을 설정에서 구분한다. 중복 요청 병합과 조건부 갱신을 우선한다. transient 오류만 backoff+jitter(대기 분산)와 총 시간·횟수 상한 안에서 재시도한다. 영구 입력·권한 오류는 중단하고 상태를 남긴다.

WebSocket 재접속은 연결 완료가 자료 완결을 뜻하지 않는다. 마지막 확인 위치 이후 REST 보충과 resubscribe·중복 처리를 거친 뒤 freshness를 회복한다. provider가 sequence·완료 증거를 제공하지 않으면 gap 해소의 한계를 상태에 남긴다. queue가 꽉 차면 read-only 갱신을 지연·생략할 수 있지만, 이미 발생한 주문·체결 기록을 조용히 버리지 않는다.

공식 키움 `kt00009` 주문체결 조회는 연속조회와 해당 TR의 초당 1건 제한을 명시한다. 이를 모든 API의 공통 제한으로 일반화하지 않는다. 구현 시 실제 사용 endpoint·quota·pagination을 다시 확인한다. [키움 주문체결 조회](https://openapi.kiwoom.com/m/guide/apiguide/08/kt00009)

## 5. 추천·배분·리밸런싱

### 5.1 추천 정책과 불변 결과

추천은 근거·목적·비교 대상·위험·유효기간을 가진 결정 기록이다. advisory domain이 자료 자격, 제약 적용, no-change 비교와 설명 가능성을 판정한다. 점수 계산은 versioned deterministic 계산을 우선한다. LLM은 출처가 연결된 설명·가설 초안에만 사용하고 숫자·실행 입력·한도는 검증한 값으로 만든다.

`Advice`는 scope, schema와 정책 revision, 근거 목록/hash, 생성·만료, 제안/대안, 비용·위험·불확실성, 데이터 누락, 결과 유형을 포함한다. 기존 제안의 내용은 수정하지 않는다. 정정 자료가 들어오면 invalidated/superseded 관계와 새 제안을 만든다. 채택·보류·메모는 별도 사건이며 제안 hash를 바꾸지 않는다.

만료·정정된 추천은 신규 거래 계획으로 수락하지 않는다. 이미 등록한 계획도 dispatch에서 현재 자료 자격·가격 범위·정책 revision을 다시 본다. 정정이 기존 근거를 무효화하면 재검토를 요구한다. 추천 정책을 바꿔 과거 추천이 더 좋아 보이도록 다시 계산하지 않는다.

추천의 거래 자격을 철회할 때 advisory application은 먼저 trading의 제한된 `BlockEvidence`를 호출한다. block의 대상은 `(owner, reference_kind, identity, version, canonical_hash, scope)`로 고정한다. 이 명령은 단조롭게 차단을 추가할 뿐 자격을 부여하거나 과거 block을 해제하지 못한다. trading이 block과 receipt를 commit한 뒤에만 advisory가 invalidated를 확정한다. 둘 사이에서 중단되면 거래는 이미 차단된 상태이며 같은 명령 receipt로 advisory 기록을 마무리한다. block 응답/commit 불명은 같은 키로 결과를 확인한다. 확인 전에는 invalidation_pending을 표시하고 철회 완료로 응답하지 않는다. 다른 owner의 임의 근거를 차단할 권한은 주지 않는다.

trading은 추천/입력 참조를 자기 계획에 저장하고 preview·승인·매 leg dispatch에서 현재 block을 검사한다. block이 먼저 commit되면 새 dispatch를 거절하고, dispatch가 먼저면 in-flight로 대조한다. TTL 만료는 trading 시계로 직접 검사한다. 정상 시세 갱신은 가격 범위/freshness 검사이고, 사용한 원문·연구의 알려진 철회/정정은 별도 자격 검사다. 원천을 몰래 바꾼 외부 파일은 즉시 관찰했다고 주장하지 않는다.

로컬에서 수락한 marketdata/research 근거의 철회·정정 자격은 각 owner DB 기록이 소유한다. admission은 해당 입력 ID/version의 자격을 **같은 transaction의 제한된 읽기**로 대조한다. 연구 파일은 미리 검증·등록한 immutable artifact만 참조하며 transaction 안에서 파일을 다시 읽지 않는다. 소스 철회를 비동기 메시지로만 전달하고 그 지연 동안 오래된 자격을 허용하는 방식은 live 기본안이 아니다.

### 5.2 실행 계획과 자금

초기 배분은 사용자가 승인한 자산 범위·목표·현금 하한·집중도·거래 비용·회전율·변경 최소 폭을 입력으로 받는다. 구체 수치와 전략 선택은 미확정이다. 이 입력이 없으면 시스템은 후보 비교를 제공할 수 있으나 actionable 자동 실행 계획을 만들지 않는다.

`ExecutablePlan`에는 계좌·provider 환경·mode·시장·통화, 목표·근거 hash, 정책 revision, 생성·만료, 최대 비용·가격/수량 범위와 **고정된 leg 목록 및 의존 관계**를 담는다. no-trade band(작은 변화는 거래하지 않는 범위)와 비용을 적용한 결과가 유지라면 주문을 만들지 않는다. KRW와 USD의 현금·수량·비용을 분리하고 exact decimal 규칙을 유지한다.

실제 외부 주문 여러 개를 한 transaction으로 체결할 수 없다. 같은 통화의 확인된 가용 자금으로 매수하고, 매도대금이 필요하면 체결·정산·비용과 provider 가용 금액이 확인될 때까지 대기한다. KRW 매도 예상액을 USD 매수 현금으로 쓰지 않는다. 자동 환전은 별도 capability·실행 계약·승인이 있어야 한다.

부분 체결·거부·가격 이탈·한도 변경 때 남은 leg를 보류하고 잔여 노출을 보여준다. 한 번 등록한 계획 안에서는 허용 범위 내의 수량 축소·중단만 가능하다. 종목·방향·수량·비용 범위가 바뀌면 새 계획이 필요하다. 개별 승인은 새 hash의 재승인이 필요하고, 지속 mandate는 현재 승인 범위와 누적 사용량을 다시 검증해야 한다. 실패한 거래를 되돌린다는 이유로 반대 주문을 자동 생성하지 않는다.

### 5.3 상태와 권한

| 객체 | 대표 상태 | 해석 |
| --- | --- | --- |
| 추천 | actionable / no_change / insufficient → expired / invalidated | 좋은 정보라도 실행 승인이 아님 |
| 실행 계획 | previewed → registered → approved / rejected / expired | 승인 대상 hash를 고정. approved도 제출 완료가 아님 |
| 실행 | pending → running → complete / stopped / unresolved | 미해결 leg가 있으면 complete 아님 |
| 주문 | 기존 상태와 unknown·부분 체결·취소·late fill | broker 사실을 계속 수용. 상위 계획을 종료해도 체결을 버리지 않음 |

개별 승인은 정확한 계획·mode·범위·한도·만료에 묶인다. 지속 mandate는 승인 주체와 scope, 허용 배분/전략 계약, 한도, 만료, 정책 revision, authority epoch를 가진다. epoch는 권한 세대를 구분하는 값이다. 자동으로 생성한 계획도 불변 hash와 mandate 참조를 저장하고 dispatch에서 재검증한다. scheduler나 연구 worker는 mandate를 발급·연장·증액하지 못한다.

자동 리밸런싱 회차는 `(mandate_id, epoch, account, policy_revision, due_window)`로 식별한다. 동일 회차의 중복 기동은 같은 계획 또는 no-change 결과로 수렴한다. partial/unknown이 남은 회차는 새 회차로 우회하지 않는다. 새 계획은 앞선 체결·열린 주문·예약과 기간 누적 회전율/비용 사용량을 포함해 검사한다. 자료 갱신마다 새 계획을 만들어 승인 예산을 반복 소비하는 경로를 막는다. 주기·기간 예산과 잔여 계획 처리 조건은 mandate 정책 입력이다.

현재 synthetic armed/lease를 실제 broker mandate라고 표시하지 않는다. 신규 실전 권한은 서버 시작·프로그램 설치·연구 승격으로 생기지 않는다. 인증된 주체도 계좌 권한과 해당 투자 승인이 별도로 필요하다.

## 6. 거래 원자성·외부 효과·복구

### 6.1 기존 보존과 새 원자적 목적

현재 paper의 주문·예약·선택·회계·authority는 기존 trading transaction에 남긴다. 기존 valuation 관측이나 owner-price import의 허가 범위를 몰래 넓히지 않는다. 아래 두 목적은 **새로운 capability(특정 작업에만 유효한 접근권) 설계 제안**이다. 실제 구현과 독립 DB 경합·복구 검증 전에는 live를 열지 않는다.

| 새 목적 | 같은 authority DB transaction에 포함 | 외부에 나가면 안 되는 것 |
| --- | --- | --- |
| execution_admission | 최신 ledger/market 사실·research 입력 자격, trading 근거 block·정책·권한·계획·예약·fence 판정, 승인 소비/실행 바인딩·예약·dispatch intent/receipt | broker 호출, 파일 생성, 무제한 retry callback, raw Tx |
| execution_result_apply | 검증한 broker fill의 trading 반영, ledger event와 중복 receipt, 예약 조정·해제 | 다른 DB의 독립 commit을 같은 commit으로 표현하는 것 |

composition은 기존 observation registry와 같은 방식으로 같은 DB·transaction·인스턴스·목적·활성 수명에 묶인 참조를 배선한다. 각 owner의 private adapter만 자기 역할의 정확한 SQL을 실행한다. admission의 trading application/domain이 금융 판정을 소유한다. 결과 반영은 trading이 검증한 체결 값 → ledger의 scoped 반영 → trading의 ledger receipt 확인 순서로 조율한다. 실패하면 전체 transaction을 되돌린다. composition은 값 변환·호출 연결만 하고 승인·수량·중복·상태 전이를 판단하지 않는다.

이때 research의 신규 scoped eligibility 조회는 입력 참조의 등록·철회/정정 상태만 반환한다. 원본 DB·파일 경로나 연구 선택 권한은 넘기지 않는다. composition이 그 값을 trading 업무 값으로 변환하므로 trading→research/advisory의 역방향 import는 생기지 않는다. advisory의 철회는 trading 소유 block이 담당한다. 자료 owner의 자격, 추천의 철회, 거래 승인 상태는 서로 다른 책임이다.

새 목적은 read-only valuation과 구분하는 새 참조/역할 검사와 정확한 statement 등록을 가진다. ledger 읽기 권한으로 ledger 쓰기를 할 수 없고, admission token으로 result apply를 호출할 수 없다. 복구·migration용 참조도 실행에 쓸 수 없다. callback·rows·참조를 저장하거나 종료 후 재사용하면 거절한다. 범용 cross-owner UnitOfWork나 임의 repository 호출 통로로 만들지 않는다.

scoped handler는 registry가 소유한 transaction을 resolve해 사용한다. 자체 transaction을 여는 기존 trading `Store.Decide/Write`를 그 안에서 다시 호출하지 않는다. 현재 단일 연결 pool에서는 중첩 기동이 대기·교착을 만들 수 있다. 기존 진입점과 새 scoped 진입점은 같은 domain 판정을 재사용하되 transaction의 시작·commit 소유자는 각각 하나다. 기존 backup 전용 trading resolver를 실행용으로 재사용하지 않는다.

이 조율은 [전체 리팩토링 설계](whole-project-refactoring.md) §15의 목적별 접근을 명시적으로 확장하는 후속 변경이다. 기존 검사가 거절하는 상태를 단순 allowlist 완화로 통과시키지 않는다. 새 public scoped 계약·owner별 proof·부정 fixture를 함께 추가해야 한다. DB 파일을 분리하려면 이 일관성 계약 자체를 다시 설계해야 하므로 금융 authority 묶음을 초기 서비스 분리 대상으로 삼지 않는다.

### 6.2 dispatch와 철회

1. broker 읽기 결과·가용 금액·자료 자격을 수집한다. 실제 확인 시각과 불완전 여부를 보존한다. 외부 broker 상태를 로컬 transaction과 동시 snapshot으로 보장하지 않는다.
2. admission transaction에서 현재 권한·epoch·위험·freshness·예약·계획 hash를 판정한다. intent·예약·`SUBMIT_DISPATCHED`에 해당하는 durable 기록을 함께 commit한다.
3. commit 성공을 확인한 writer만 broker 호출을 시도한다. broker의 검증된 idempotency가 없는 한 외부 effect를 재시도하는 자동 callback에 넣지 않는다.
4. 응답·조회로 결과를 기록한다. timeout·응답 유실·commit 불명은 unknown이며, 동일 범위의 위험 증가/충돌 주문을 차단하고 대조한다.

철회가 admission commit보다 먼저면 이전 epoch dispatch를 거절한다. dispatch가 먼저면 이미 허가된 in-flight로 분류하고 취소·대조를 수행한다. 철회 receipt는 새 dispatch 차단의 commit을 뜻하며 이미 보낸 주문을 소급 취소했다는 뜻이 아니다. 이 경계를 UI에도 그대로 표시한다.

### 6.3 가용 금액·체결 식별·late fill

local ledger는 기록한 금융 사실이고 broker의 주문 가능 금액은 외부 가용성이다. 둘을 같은 잔고라고 가정하지 않는다. 보수적으로 broker의 확인 가능한 가용 금액과 로컬 한도·미확정 예약을 함께 제한한다. broker 수치에 예약이 반영됐는지 모르면 해제를 추정해 자금을 더하지 않는다. 이중 차감으로 거래를 놓치는 편을 택하고 원인을 알린다.

앱 밖의 수동 거래·입출금은 조회와 제출 사이에 경쟁할 수 있다. 초기 live profile은 통제된 전용 계좌 또는 외부 행위 통제를 운영 조건으로 요구한다. 그렇지 않으면 앱이 계좌 전체의 한도를 절대 보장한다고 표시하지 않으며 자동 실행을 허용할 범위를 다시 정해야 한다. 최종 broker 거부도 정상적인 실패 경로다.

체결 dedup은 검증한 `(provider, environment, account, trading_date, provider_order_id, provider_execution_id)` 범위와 canonical 내용 hash를 사용한다. provider의 실제 식별 범위가 다르면 adapter capability와 fixture로 먼저 증명한다. 같은 identity·같은 내용은 기존 결과를 재사용하고, 다른 내용은 격리·계좌 차단 후 대조한다. 식별자를 확보하지 못한 체결을 추정 키로 실전 반영하지 않는다.

취소 요청·halt·철회·상위 실행 중단 뒤의 late fill도 사실로 기록한다. 예약은 timeout이나 취소 접수로 해제하지 않는다. 확정된 체결·잔량·취소와 provider 계약이 증명하는 부분만 조정한다. result apply 실패는 실제 체결을 취소하지 못한다. durable 수신 기록과 broker 재조회로 같은 체결을 다시 적용한다.

### 6.4 대조와 writer 인계

한 번의 `not found`나 빈 페이지는 미제출 증명이 아니다. 계좌·환경·주문 alias·조회 기간·연속조회 완결·체결/미체결·현금을 대조한다. 자동 해소 근거가 부족하면 unknown과 차단을 유지한다. 운영자 해소도 원인·증거·주체·결과를 기록하며 위험 기록을 삭제하는 버튼으로 구현하지 않는다.

DB lease·fence는 DB의 오래된 writer를 차단한다. broker가 그 token을 검사하지 않으면 외부 주문을 fencing하지 못한다. 초기에는 단일 활성 gateway, process identity와 독점 writer, 감독 프로세스로 중복 기동을 막는다. 인계는 신규 행동 차단 → 이전 process/연결 종료 확인 → in-flight 대조 → 새 authority epoch → 별도 활성화 순서다.

이전 host가 살아 있는지 알 수 없거나 network partition으로 외부 호출을 배제할 수 없으면 대체 live writer를 시작하지 않는다. provider·credential·network 차단을 실제로 증명할 수 있는 수단을 마련하기 전에는 다중 host 자동 failover를 지원하지 않는다. 가용성보다 중복 외부 효과 방지를 우선하는 명시적 선택이다.

## 7. 성과·복기·투자 자동 개선

### 7.1 비교 가능한 성과

복기는 추천 당시의 근거·실행 계획·승인·주문과 실제 결과를 연결한다. 거래하지 않은 추천과 사용자가 보류한 제안도 남기되 관측한 사실과 가정 결과를 분리한다. 반사실 모의 결과를 실제 수익으로 합산하지 않는다.

| 질문 | 계산·표시 기준 |
| --- | --- |
| 얼마를 벌거나 잃었는가 | 원통화 실현/미실현, 현금흐름, 수수료·세금·FX 비용·슬리피지·가격 기준 명시 |
| 전략 판단이 나았는가 | 동일 기간·통화·배당/기업행사·비용 기준의 보유 유지/현금/승인 benchmark와 비교 |
| 위험을 얼마나 썼는가 | 최대 손실폭, 노출·집중도·회전율·현금 사용, 표본 수와 기간·불확실성 |
| 실행에서 무엇을 잃었는가 | 계획 가격과 실제 가격, 지연·미체결·부분 체결·시장 충격·모형 차이 |
| 돈을 넣고 뺀 영향인가 | TWR(현금흐름 영향 분리)와 XIRR(투입 시점 포함)을 구분 |

성과 attribution(원인별 분해)은 합계와 잔차를 대조한다. 시장·전략·FX·비용을 분리할 근거가 부족하면 잔차/판단 불가를 표시한다. 근거가 없는 원인 설명을 LLM으로 채우지 않는다. 가격·수수료·세금·기업행사가 미완성인 구간은 최종 순수익이라고 표시하지 않는다.

### 7.2 연구 등록과 선택 편향

study는 실행 전에 가설·대상 universe·데이터 cutoff/version·후보 공간·선택 기준·비교 정책·비용 모형·train/validation/holdout과 자원 예산을 고정한다. 각 candidate/fold/attempt는 계산 전에 started를 기록하고 완료·실패·취소·가지치기 결과를 남긴다. 성공 파일만 모으지 않는다.

연구 family는 같은 가설·겹치는 데이터·수동 조정·재시도 관계를 연결한다. 새 study ID를 발급해 holdout 사용 이력을 초기화하지 않는다. 과거 수동 시도를 모두 알 수 없다면 그 한계를 표시하고 전체 탐색 기록을 증명했다고 하지 않는다.

Python의 기존 walk-forward와 final holdout을 재사용하되, 모든 fold의 모든 후보 기록을 현재 구현이 완전하게 가진다고 가정하지 않는다. 시간 경계를 넘는 label/보유 구간의 누출을 검사한다. 발표·정정 이후 자료, 미래 구성 종목, 상장폐지 누락과 조정 가격 혼용을 점검한다. 코드·입력·seed·계산 version·비용 모델을 manifest에 고정한다.

holdout 접근은 기준을 고정한 뒤 기록된 절차로 수행한다. 결과를 본 후 같은 기간으로 다시 선택한 결과는 독립 검증이 아니다. 다음 미관측 구간 또는 사전에 정한 별도 검증 절차가 필요하다. 여러 후보를 비교해 고른 결과는 선택 편향을 가진다. PBO(백테스트 과적합 가능성) 등 통계 방법은 자료와 연구 설계에 맞춰 선택하고, 점수 하나를 자동 실전 승인으로 쓰지 않는다. [Bailey 외, The Probability of Backtest Overfitting](https://www.davidhbailey.com/dhbpapers/backtest-prob.pdf)

### 7.3 개선 후보의 승격

1. 복기에서 반증 가능한 가설과 실패 이유를 만든다.
2. 예산 안에서 연구하고 비용 스트레스·구간별 안정성·민감도와 기준 전략을 비교한다.
3. 데이터/계산 적합성, 전체 trial·holdout 이력, 적용 가능한 시장·수량·비용 모델을 검사한다.
4. 다음 미관측 구간의 paper/shadow에서 주문·회계·운영 결과를 관찰한다.
5. trading의 version별 admission과 기존 승격 gate를 거쳐 후보를 등록한다.
6. 이미 승인한 mandate 범위 안의 활성화인지 확인한다. live 신규 전환·자금 확대는 사용자 별도 승인이다.

현재 SMA 1.0.0 gate에 다종목 배분을 다른 이름으로 끼워 넣지 않는다. 신규 rebalance 모델에는 별도 schema·정산·수량·가격·기업행사·부분 체결 적합성 검사가 필요하다. 기존 v1/v2 연구 형식과 원래 canonical bytes/hash를 보존하고 신규 지원을 명시적으로 추가한다.

자동 개선의 목적 함수는 미확정이다. 우선 원금·위험·비용·연구 예산 제약을 만족한 후보만 비교하는 구조로 설계한다. 사용자의 목표 기간·허용 손실·회전율·비교 기준이 정해지기 전에는 하나의 최고 수익 점수로 자동 선택하지 않는다.

### 7.4 연구 프로세스·예산·취소

Go research application이 durable job·attempt·lease·artifact receipt를 소유하고 adapter가 Python 프로세스를 실행한다. study 입력·출력 경로, 실행 version, CPU/메모리/시간·동시성·저장/출력량·외부 비용의 상한을 검증한다. 필요한 값이 없으면 무제한 실행하지 않는다. 외부 유료 호출은 승인한 예산·공급자만 허용한다.

worker는 한 작업의 제한된 읽기 입력과 쓰기 출력만 받고 운영 DB·주문 secret·임의 네트워크 권한을 받지 않는다. 도구가 지원하지 않는 격리는 보장 목록에서 제외한다. 종료 요청은 새 작업 차단 → 취소 전파 → 자식 process group 종료/대기 → 상태·부분 artifact 보존 → 소유 자원 정리까지 확인한다. PID만 재사용해 다른 프로세스를 죽이지 않는다.

작업 lease가 만료돼도 이전 attempt 결과를 현재 결과로 덮지 못하게 fencing한다. immutable artifact는 동일 hash로 재사용할 수 있지만 작업 성공은 현재 attempt/정책 검증 뒤 확정한다. 실패한 연구 작업은 broker 주문을 재시도하지 않는다.

## 8. 제품 개선·확장·공급자 교체

### 8.1 제품 개선 흐름과 차별성

제품의 차별성은 데이터 소유권을 보존한 **결정의 재현성**에 둔다. 추천·보류 → 당시 근거 → 승인한 범위 → 실제 결과 → 비용·실패 이유를 한 흐름으로 찾을 수 있어야 한다. 기사를 많이 보여주거나 LLM 설명이 길다는 사실을 유효한 차별성으로 세지 않는다.

사용자에게 보이는 지연·오류·반복 행동·확인 시간을 측정하고 작은 개선 가설을 세운다. 제품 개선 자동화는 이 근거로 issue/설계/실패 테스트/변경안을 만들 수 있다. 실제 변경은 별도 작업 branch의 TDD·리뷰·CI·허가된 배포 절차를 따른다. 운용 프로세스가 자기 주문 안전 정책·credential·한도를 수정하거나 스스로 배포하지 않는다.

UI는 기존 feature·application·adapter 경계를 유지한다. 신규 추천·계획·복기 화면에서 loading, empty, no-change, insufficient, stale, expired, 권한 없음, offline, partial, unknown을 구분한다. 마지막 정상 값을 보여주면 기준 시각과 갱신 실패를 표시한다. paper/live는 색상에만 의존하지 않고 글자로 표시한다. 키보드·screen reader·작은 화면과 오류 후 재시도/receipt 조회를 검증한다.

### 8.2 저장·cache·메시지 확장

설정에서 의미 있는 인스턴스 ID와 역할을 지정한다. 같은 종류의 provider 여러 개는 source/environment/account와 우선순위·quota가 다를 수 있다. 대체 공급자 자료를 합칠 때 품질·시각·adjustment가 호환되지 않으면 조용히 교체하지 않는다. domain/application에 driver 이름 switch를 추가하지 않는다.

| 필요 | 초기 선택 | 전환을 검토할 근거 |
| --- | --- | --- |
| 금융 authority | 같은 SQLite DB의 단일 writer | 독립 다중 writer·HA/PITR 요구와 실제 경합 → PostgreSQL 계획 |
| 원문·연구 대형 파일 | 제한된 로컬 파일과 manifest | 용량·보존·원격 복구 요구 → object storage adapter |
| 조회 cache | 필요한 재계산 결과만 메모리/파일 | 실제 계산·지연 증거 → 공유 cache. 권한·잔고·예약 정본으로 사용 금지 |
| 비동기 작업 | 실제 작업에만 DB job/outbox | 처리량·격리 필요 → job queue adapter/RabbitMQ |
| 사실 replay·여러 소비자 | 필요한 durable 사실/outbox | 소비자 수·재처리 요구 → event log adapter/Kafka |

cache key에는 workspace/scope·통화·정책/자료 revision·계산 version을 넣고 TTL·무효화·오류 시 동작을 정한다. 키 분리는 자원·부하 격리를 뜻하지 않는다. cache 장애는 읽기 지연/불가이며 권한 허용으로 전환하지 않는다.

Kafka의 사실/replay와 RabbitMQ의 작업/ACK를 하나의 범용 MessageBus로 숨기지 않는다. DB effect+inbox 또는 durable job handoff 뒤에만 소비 완료를 확인한다. outbox 전달·job ACK는 업무 완료와 다르다. 두 broker를 함께 쓰면 동일 업무 명령의 owner와 dedup 키를 유지한다. 과거 사실 replay가 주문·알림을 다시 실행하지 않게 소비 경로를 분리한다. 상세 보장은 기존 [플랫폼 확장 설계](scalability-and-platform.md) §6을 따른다.

### 8.3 공급자와 통화 capability

| 능력 | 계약으로 확인할 것 | 지원하지 않을 때 |
| --- | --- | --- |
| 시장 자료 | 시장·상품·시각·조정·정정·quota·사용권 | source별 사용 범위 제한 |
| 주문/조회 | mock/live, 시장·통화·수량 단위, 주문 종류, 식별·대조 범위 | 해당 조합 기동/실행 거부 |
| 취소·정산 | 부분 취소·late fill·잔량·휴장·결제와 주문 가능 금액 | 미확정 유지·위험 증가 차단 |
| 재시도·fence | provider idempotency와 실제 보존 범위, 외부 writer 차단 | exactly-once/자동 failover를 지원한다고 하지 않음 |
| 환전 | 환율 견적·비용·체결·결제·권한 | 자동 FX leg 생성 금지 |

현재 키움 중심 개발과 KRW/USD 제품 목표를 구분한다. 키움 또는 다른 한 공급자가 두 시장의 모든 기능을 지원한다고 가정하지 않는다. USD 조회·평가·연구와 USD live 제출은 다른 capability다. 공급자·거래 유형별 실제 인증 관찰이 필요하다.

### 8.4 사용자 확장 계약의 적용 범위

현재 제품에 임의 사용자 코드를 설치·실행하는 호스트가 필요한 근거는 없다. 먼저 내장 provider·전략 구현을 versioned 입력/출력·적합성 검사로 연결한다. 설치 가능한 확장이 실제로 필요해지면 내장 구현도 같은 검증 계약을 통과하게 한다.

그때 manifest에는 ID·호환 version·요청 capability·자료 출처·자원/비용 예산·수명·취소·출력 schema를 둔다. 설치는 실행 승인이나 권한 부여가 아니다. host가 별도 grant와 제한된 process/네트워크/파일 권한을 부여하고 철회·종료를 책임진다. strategy/research 확장에는 broker credential·운영 DB·임의 주문 권한을 주지 않는다. 장래 확장 가능성을 이유로 지금 일반 plugin framework를 만들지 않는다.

## 9. 운영·보안·관측·복원

### 9.1 운영 profile과 접근 경계

| profile | 용도 | 열기 전 gate |
| --- | --- | --- |
| 개인 로컬 | loopback Go + SQLite 단일 writer, Flutter, 제한된 Python | local 권한·프로세스·실행/복구·자료 보존 검증 |
| 무인 paper/shadow | 지속 수집·제한된 연구·알림·모의 실행 | 감독·drain·재시작·backup/restore·알림 실패·예산 관찰 |
| private remote P2a | 한 Go writer와 local/RWO DB, 인증된 개인 접근 | OIDC/TLS·secret·계좌 권한·원격 운영/복구 검증 |
| bounded live | 지원하는 provider/계좌·개별 승인 또는 제한된 mandate | 실제 mock/shadow 대조, §6 원자성·fence 한계 해소, 별도 사용자 활성화 |

profile과 거래 mode는 별도 축이다. 로컬에서도 live는 별도 gate가 필요하고 원격이라고 live 권한이 생기지 않는다. 개인 production에 PostgreSQL/Kubernetes가 필수는 아니다. 기존 G6의 다중 writer/배포 승격은 그 요구가 확인됐을 때 적용한다. 기존 중지/skip한 VM 작업은 이 설계로 재개하지 않는다.

현재 loopback은 OS 로컬 사용자를 신뢰하는 경계다. 인터넷 bind만 바꾸지 않는다. 향후 원격 profile은 검증한 OIDC(외부 신원 제공자 인증), TLS, 계좌별 권한과 감사가 없으면 시작을 거절한다. native client는 Authorization Code+PKCE, web은 BFF/보안 HttpOnly cookie/CSRF 보호를 기존 목표로 유지한다. 직접 비밀번호·token 서버를 새로 만들지 않는다. [OAuth 보안 권고 RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html)

인증 성공과 거래 승인은 다르다. viewer/researcher/operator/owner/service principal 같은 역할은 원격 구현의 목표이며 현재 구현된 권한 체계라고 표시하지 않는다. JWT role만으로 mandate를 허가하지 않고 trading authority DB의 현재 grant·revocation epoch를 dispatch에서 확인한다.

### 9.2 secret·불신 입력·감사

broker secret은 Go의 credential adapter만 읽는다. Flutter·Python·Mimir·LLM·추천 artifact·log에는 넣지 않는다. 원격 secret manager 선택은 운영 환경이 정해진 뒤 한다. mock/live secret과 endpoint·계좌 identity를 분리하고 잘못된 환경 결합은 시작 시 거절한다.

기사·공시·model output·외부 파일은 데이터다. 그 안의 명령을 실행하거나 권한 요청으로 해석하지 않는다. 네트워크·파일 경계에서 크기·schema·출처·path를 검증한다. 서버가 정한 allowlist 밖의 URL·경로를 연구 도구에 전달하지 않는다. 외부 AI/데이터 공급자에 개인 계좌·비공개 원문을 보내려면 사용권·노출 범위·비용 승인이 선행해야 한다.

감사는 주체·작업·scope·입력/결과 hash·정책 revision·승인·거절·시각·인과 ID를 남긴다. 계좌 raw payload는 최소화·redaction하고 접근·보존 정책을 적용한다. 알림은 실행 상태의 참조와 최소 정보만 전달한다. 알림 실패·중복이 주문 재실행을 만들지 않는다.

### 9.3 기동·종료·자원 수명

기동은 schema/profile/capability/자료 경로·예산·authority 상태를 확인하고, 미완료 intent·job·unknown·복원 여부를 판정한 뒤 읽기를 연다. 장기 backfill이나 전체 연구를 readiness 기동 시간 안에 실행하지 않는다. 준비되지 않은 거래는 별도 blocked 상태다.

종료는 신규 주문·작업 접수 차단 → scheduler 정지 → 진행 중 DB 처리/receipt 보존 → HTTP·worker drain → broker in-flight 상태 보존·가능한 대조 → 자식·연결·파일·pool 종료와 대기다. context cancel을 호출했다는 이유로 종료 완료라고 하지 않는다. 제한 시간을 넘기면 미확정 상태와 복구 지점을 남긴다. 실제 broker 전송이 없었다고 추정하지 않는다.

process·goroutine·구독·timer·WS·연결·파일·lease의 생성자가 수명을 소유한다. 연구는 장기 DB transaction을 점유하지 않고, 읽기 API도 batch/기간 한도를 갖는다. 디스크·DB 압박 때 새로운 연구·원문 수집을 제한하고 주문 결과·철회·대조 기록에 필요한 여유를 우선 확보한다. 실제 여유 수치는 측정 후 설정한다.

### 9.4 관측과 운영 목표

관측 ID는 study/advice/plan/command/execution/order/fill을 연결한다. 고유 ID는 로그·trace에 두고 metric label은 제한해 cardinality 폭증을 막는다. liveness는 프로세스 생존이고, readiness·자료 freshness·거래 가능성은 다른 상태다.

| 신호 | 사용자/운영 의미 | 기본 대응 방향 |
| --- | --- | --- |
| 자료 age/gap·정정 backlog | 추천 근거가 낡거나 불완전 | 신규 추천/dispatch 차단 또는 불가 표시 |
| unknown age·대조 lag·예약 차이 | 실제 노출을 확정할 수 없음 | 위험 증가 차단, 대조·운영자 알림 |
| writer 경합·DB 지연·disk 여유 | 금융 기록을 제때 보존하지 못할 위험 | 새 비필수 작업 유입 제한, 결과 기록 우선 |
| job backlog·budget·자식 process | 연구가 운용 자원을 침해 | 신규 연구 중단·소유 attempt 취소/종료 확인 |
| backup age·restore 리허설·감사 누락 | 복구를 증명할 수 없음 | live 신규 활성화 차단·복구 증거 보완 |
| 알림 delivery·dedup | 중요한 상태가 전달되지 않음 | 별도 전달 복구. 주문 재실행 금지 |

SLO(허용 지연·가용성 목표), RPO(허용 데이터 손실), RTO(복구 시간)는 아직 미정이다. 개인 paper 운용에서 측정하고 자금 위험과 맞춰 확정한다. 처음부터 99.99%·RPO 0 등을 선언하지 않는다. 숫자·관측 창·알림 경로가 없는 상태는 해당 production gate의 미완료다.

### 9.5 backup·restore·rollback

현재 schema 27·backup 21 및 지원 legacy의 canonical bytes·hash·proof를 보존한다. 새 owner와 상태는 migration·건수/hash·관계·권한 상태 검증을 추가한다. DB·WAL·원문·artifact의 일관성은 기존 backup 절차와 manifest로 확인하고 live DB 파일만 임의 복사하지 않는다. backup은 secret·권한·암호화·독립 복구 위치를 함께 관리한다.

복원 리허설은 독립 사본에서 한다. 복원 DB는 **자동 실행 비활성**으로 시작하며 옛 armed/mandate·lease를 부활시키지 않는다. 이전 writer 종료/차단을 확인하고 새로운 runtime authority epoch를 부여한다. broker의 현재 미체결·체결·현금과 backup 이후 기록을 대조한 뒤 사용자가 재활성화한다. broker에서 재구성하지 못한 감사·승인·원문은 복구 불가로 남기며 만들어 채우지 않는다.

runtime epoch를 같은 backup의 과거 값으로만 복원하면 외부 writer를 차단하지 못한다. 실전 재활성화는 새 시작에서 비활성화한 상태, 이전 writer의 종료/외부 차단, 새로운 권한의 명시적 발급을 모두 요구한다. 외부 차단을 증명하지 못하면 failover하지 않는다.

프로그램 rollback은 이미 체결된 거래를 되돌리지 못한다. 새 schema에 쓰기 시작한 뒤 옛 binary/DB로 바로 돌아가지 않는다. 안전한 중단·읽기 전용·호환 binary 또는 검증한 forward fix를 우선한다. backup 손실 범위가 허용 RPO를 넘으면 live를 열지 않는다.

## 10. 데이터 모델·조회·보존 설계

다음은 새 데이터 책임의 **논리 모델**이다. 테이블/인덱스 이름은 구현 전 migration 검토에서 확정한다. 기존 테이블을 새 owner가 직접 읽거나 새 hash 규칙으로 재작성하지 않는다.

| owner / 논리 기록 | identity·불변 조건 | 주요 조회·제약 |
| --- | --- | --- |
| marketdata / source observation·cursor | source scope+ID+version, 내용 hash; 정정은 새 version | 종목/자료 종류/이용 가능 시각/관측 시각, source cursor의 유일성 |
| research / study·trial·artifact | family+study+candidate+fold+attempt; 시작 전 등록 | study/status/created_at, 중복 요청 unique, holdout 접근 이력 |
| advisory / policy·evidence·advice·review | revision, immutable hash, supersedes, 이용한 입력 refs | scope/생성 시각, 추천/실행 참조, keyset pagination |
| trading / executable plan·approval·mandate·evidence block | 계좌/env/mode·hash·revision·epoch·만료·승인 소비 execution | command key·승인 소비 바인딩 unique, 현재 grant/block·미해결 조회 |
| trading / execution·leg·intent·fill receipt | 기존 order identity + 새 plan/leg 관계 | plan당 execution unique, execution/leg당 intent unique, 계좌/status·order alias·provider fill scoped unique |
| ledger / broker fill event receipt | provider fill scoped identity+hash, 원장 event와 함께 commit | fill receipt unique, ledger revision·event 원래 순서 보존 |

최근 목록은 `(created_at, id)` 같은 안정된 cursor로 페이지를 나눈다. 미해결·대조 대상은 계좌/상태와 갱신 순서로 제한 조회한다. 기간 집계·상태 조회·dedup 실제 SQL에 맞춰 composite index와 unique 제약을 만들고 실행계획·최대 입력 검사를 남긴다. 구현 전 추정만으로 인덱스를 일괄 추가하지 않는다.

cross-owner 관계는 업무 ID/hash와 owner의 검증 계약으로 연결한다. FK/trigger를 쓰더라도 다른 owner의 쓰기 정책이나 cascade delete를 만들지 않는다. maintenance의 동일 관측 proof가 연결 관계를 대조한다. 추천이나 연구 원문 정리가 금융 감사·승인·체결 참조를 끊지 않게 최소 manifest와 hash/receipt를 보존한다.

자료 권리·개인정보·연구 재현·감사 보존을 나눠 기간과 삭제 조건을 정한다. 원문 사용권이 만료되면 원문 접근/삭제 정책을 적용하고 남길 수 있는 최소 파생 metadata를 확인한다. hash가 있다는 이유로 사용권을 영구 보유한다고 가정하지 않는다. 재현 불가가 된 보고서에는 그 한계를 표시한다.

## 11. 호환성·이전·서비스 확장

### 11.1 기존 계약을 보존하는 적용

새 schema/contract는 기존 v1/v2와 공존한다. 기존 canonical JSON·decimal·hash·query·오류·event sequence·legacy 복구를 조용히 바꾸지 않는다. 새로운 다종목·시장 capability는 새 version/admission으로 추가한다. 엄격한 decoder를 가진 Go/Python/Flutter에 필드를 무작정 추가하지 않는다. OpenAPI·JSON schema·양/음성 fixture·producer·consumer를 같은 수직 단위에서 갱신한다.

신규 plan/mandate scope는 현재의 global strategy selection/sequence와 격리한다. 하나의 실제 계좌/환경/mode에는 활성 실행 regime가 하나만 권위를 가지게 한다. 기존 runner와 새 rebalancer가 같은 자금을 따로 승인하지 못하도록 공통 authority/reservation 검사 또는 명시적인 상호 배타 profile을 먼저 증명한다. migration 전에는 새 runner를 켜지 않는다. 기존 전역 값을 계좌별로 자동 복제하거나 새 tenant 상태로 해석하지 않는다.

### 11.2 데이터 이전 순서

1. 최소 지원 binary/schema·쓰기 주체와 legacy 계약을 고정한다. backup/복구 proof를 확보하고 새 writer는 비활성화한다.
2. additive schema·owner별 SQL grant·backup proof·contract parser를 적용한다. 새 데이터 쓰기가 시작되면 구 binary가 기동/쓰기하지 못하는 version fence를 검증한다.
3. 필요한 historical 입력만 bounded backfill한다. source revision/hash를 조건으로 오래된 값이 최신 정정을 덮지 않게 한다. 원래 bytes와 시각의 미확인을 보존한다.
4. ledger·reservation·plan·artifact 관계와 건수/hash·누락을 대조한다. 새 조회를 shadow로 비교한 뒤 읽기를 전환한다.
5. 기존/신규 runner 상호 배타와 권한을 확인한 뒤 paper만 연다. 신규 live는 독립 gate와 승인 뒤 연다.

모든 변경에 dual-write를 강요하지 않는다. 작은 additive 변경은 단일 migration으로 충분하다. 실제 온라인 backfill/cutover가 필요할 때만 단계와 즉시 중단·조회 복귀 조건을 구체화한다. 금융 상태를 서로 다른 DB에 이중 쓰는 방식은 기본안이 아니다.

### 11.3 DB·서비스·MQ 확장 조건

처음에는 Go 모듈형 단일 서비스다. 수집·연구 계산처럼 불변 artifact로 연결 가능한 부하를 먼저 분리할 수 있다. 주문·권한·예약·회계의 동시 일관성 묶음은 분리 비용이 크다. 모듈 수나 폴더 수로 MSA 준비 완료를 주장하지 않는다.

PostgreSQL은 다중 독립 writer/HA/PITR·실제 writer 경합이 필요할 때 검토한다. DB 교체는 API 맞춤뿐 아니라 isolation·unique 경합·rollback·pool·취소·replay·복구 semantics를 실제 backend contract suite로 검증해야 한다. Kafka/RabbitMQ 동시 사용은 기존 플랫폼 설계의 destination별 outbox/inbox·순서·ACK·replay 계약을 따른다.

adapter 추가는 기존 업무 규칙을 바꾸는 우회로가 아니다. 지원하는 profile 조합만 CI matrix와 Podman 실제 통합 검사에 넣고, 미검증 조합은 시작 시 거절한다. tenant 확장은 별도 보안·scope·migration 작업이며 지금 account 필드 하나로 완료했다고 하지 않는다.

## 12. 구현 단위·TDD·검증·완료 gate

### 12.1 다음 구현 순서

아래는 **실행하지 않은 계획**이다. 각 단위는 변경한 행동의 실패 검사 → 최소 구현 → 리팩토링 → 영향 검사 → 문서/독립 리뷰 → 작업 소유 커밋의 순서다. 기존 동작 이식은 특성화 검사로 고정한다. 사용자가 이미 승인한 Git 전달 범위는 실제 구현 시 보호 규칙·필수 CI를 따라 적용하며, 이 문서가 배포나 실전 승인을 대신하지 않는다.

| 단위 | 사용자에게 주는 결과 | 주요 변경 경계·선행 조건 | 완료 gate |
| --- | --- | --- | --- |
| I1 자료·평가 근거 | KRW/USD 원통화와 누락/시각을 신뢰할 수 있음 | marketdata·portfolio·versioned evidence·기존 UI | 같은 Tx/cutoff, 시점/정정/통화/누락 계약, 수집 중단 재개·cursor 실제 DB |
| I2 추천 첫 흐름 | 근거·비용·유지 대안을 보는 불변 추천 | advisory 신규, composition 배선, client, architecture/backup | stale·같은 키 충돌·정정·채택이 주문을 만들지 않는 API/화면 |
| I3 연구·복기 | 모든 시도와 비용을 보존한 비교·다음 가설 | research job/trial, Python artifact, advisory review | holdout 재사용/누출·실패·취소·예산·과거 bytes, 부분 결과의 불가 표시 |
| I4 모의 리밸런싱 | 고정된 다종목 계획 승인·실행·잔여 확인 | trading 새 모델·scope/runner migration, client | 부분 체결·예약·취소/late fill·중복·경합·KRW/USD 수량 모델의 실제 SQLite 검사 |
| I5 무인 운용 | 종료·재시작·복원·알림 실패에도 상태를 신뢰 | composition serve/runner, maintenance, process/관측 | drain·강제 종료·restore 비활성·disk/예산·정리·운영 측정. I1부터 필요한 lifecycle 수정은 앞당김 |
| I6 실제 연결·live 준비 | 실제 mock/shadow 관찰과 제한된 실전 승인 가능 | 공급자 capability, §6 두 새 목적, 계좌·위험·복구 정책 | 실제 인증·전 페이지 대조·same-Tx/철회/unknown/fence·RPO/RTO와 독립 리뷰·별도 사용자 활성화 |

첫 사용자 결과는 I1→I2다. 실전보다 먼저 모의 전체 흐름을 닫는다. I5의 필수 종료/예산 안전은 장기 worker를 도입하는 단위부터 적용하며 나중으로 미루지 않는다. I6의 ledger·예약 원자성 설계는 이 문서에 포함하지만 구현 전 증명을 남긴다. I6이 끝나도 비용 차감 수익의 우월성은 별도 운용 관찰이다.

### 12.2 변경 경로와 문서

`advisory/`, study/rebalance schema와 새 테스트 이름은 제안 경로다. 없는 파일을 현재 검사 근거로 인용하지 않는다. 전체 변경 목록과 검증 연결은 부록 C에 둔다. 기존 `docs/architecture/whole-project-refactoring.md`와 `scalability-and-platform.md`를 대체하지 않고 후속 기능·운영 설계의 정본은 이 문서가 맡는다.

각 구현 단위는 공개 계약·inbound·application·domain·adapter·composition·클라이언트와 fixture를 함께 추적한다. migration·SQL grant·backup proof와 해당 gate 문서도 영향이 있을 때 같은 단위에서 갱신한다. 현재 PLAN/GATES의 미완료 항목을 이 설계 문서 작성만으로 체크하지 않는다.

### 12.3 검증 계층과 실제 명령

| 계층 | 확인할 내용 | 증거 조건 |
| --- | --- | --- |
| domain 단위 | 자료 자격·추천 결과·비용/위험·상태 전이·정확한 수량 | 구현 복제가 아닌 입력과 업무 결과, 경계·반례 |
| API/교차언어 계약 | schema/version·권한·scope·키/hash 충돌·진행/불명·consumer 호환 | Go/Python/Flutter의 같은 양/음성 fixture, 기존 `make contract-check` |
| 실제 DB/프로세스 | 같은 Tx·경합·rollback·cursor·부분 실패·restore·종료/누수 | 실제 SQLite·별도 연결/프로세스·실패 주입. fake 통과로 대체 금지 |
| E2E/접근성 | 근거→채택→승인→paper→복기의 주요 사용자 결과 | loading/empty/error/offline/unknown, 키보드·작은 화면 |
| 공급자 통합 | mock/live 각각 identity·query 완결·WS gap·submit/cancel/fill | 실제 인증 관찰과 synthetic/fake 검사를 구분 |
| 운영 | rollout·인계·drain·backup/restore·알림·자원 예산 | 실행한 profile·revision·환경·관측 창·실제 결과 |

구현 최종 diff에서는 root `make check`를 한 번 수행하고 영향받는 HTTP 업무 흐름에 `make smoke`를 실행한다. 필요한 집중·Podman backend 검사는 그 전에 수행하고 같은 소스·설정·환경의 유효한 근거는 재사용한다. Docker Desktop을 요구하지 않는다. 현재 지원 backend는 SQLite이며 PostgreSQL/MQ suite는 adapter를 실제 추가하는 시점의 조건이다.

검증은 작업별 임시 경로·process identity·container label로 소유권을 남긴다. 성공·실패 뒤 자식·연결·컨테이너·볼륨·개인 캐시·임시 artifact를 정리하고 잔여를 확인한다. 원인/재현 근거는 기존 보관 경로에 먼저 남긴다. 공유 SDK/module cache·다른 작업 리소스는 삭제하지 않고 일괄 prune을 하지 않는다.

### 12.4 출시 판정

| 상태 | 필요한 증거 | 이 문서 작성 시점 |
| --- | --- | --- |
| 설계 | 계약·owner·실패/이전·추적·독립 검토 | 제안 문서. 검토 결과는 작업 기록에서 별도 관리 |
| 구현 | 해당 단위의 producer/consumer·fixture·문서가 함께 변경 | 신규 기능 미구현 |
| 로컬 검증 | 해당 최종 소스의 실제 명령·exit code·범위·정리 | 신규 기능 미실행 |
| CI | 정확한 head의 필수 검사 성공 | 신규 기능 CI 없음 |
| 배포 | 허가된 환경·profile의 rollout·복구 확인 | 미실행 |
| 실전·성과 | 계좌·mandate·위험/복구 승인과 실제 관찰 | 미확인. 백테스트/모의 결과로 대체하지 않음 |

## 13. 대안과 결정 이유

동일한 소스·목표로 두 구조를 비교했다. 선택은 후보 B의 advisory 경계를 기본으로 하고, 후보 A의 금융 상태 응집·같은 관측·기존 promotion 보존을 적용한 합성안이다. 아직 사용자 정책 승인이나 구현 결과는 아니다.

| 대안 | 이점 | 비용·위험 | 판단·재검토 조건 |
| --- | --- | --- | --- |
| A 기존 7 owner만 확장 | 새 owner/backup 비용 작고 금융 상태가 trading 안에서 닫힘 | 추천 정책·비실행 결정·복기·연구 실행까지 trading에 모일 위험 | 추천이 일회성 조회 조합으로 줄어들면 채택 가능 |
| B advisory 1개 추가 | 추천·유지·보류·복기를 거래 권한 없이 발전시킴 | artifact handoff·만료·receipt·새 owner proof 필요 | 채택. trading 독립 검증·acyclic 전달을 조건으로 함 |
| 단계별 수집/점수/추천/주문 서비스 | 개별 배포 가능 | 자료·정책 복제, 네트워크 실패·얕은 전달 계층과 금융 정합성 비용 | 거절. 측정된 독립 운영 책임이 생길 때만 분리 |
| 범용 plugin/MessageBus/Repository | 겉보기 교체 지점 통일 | 실제 권한·순서·ACK·transaction 차이를 감춤 | 거절. 구체적인 의미와 작은 계약 유지 |
| 금융 owner별 독립 DB부터 도입 | DB 선택 자유 | 승인·예약·ledger 반영의 새 분산 일관성 문제 | 초기 거절. 필요한 일관성 증명과 운영 근거가 생길 때 재설계 |

후보의 경계 선택만으로 live가 안전해지지 않는다. 두 후보가 남긴 실전 자산·원장 반영의 공백은 §6의 명명된 원자적 목적과 실제 검증 gate로 구체화했다. 외부 broker exactly-once와 자동 failover는 여전히 증명할 수 없는 기본 가정으로 두지 않는다.

## 14. 남은 정책·확인과 구현 진입 조건

아래는 사용자가 밝혀야 하는 자금·비용·운영 책임 선택과 도구로 조사할 사실을 구분한 목록이다. 기존 합의를 다시 인터뷰하지 않는다. 비밀값은 문서나 대화에 기록하지 않는다.

| 항목 | 종류·필요한 입력 | 없을 때의 gate |
| --- | --- | --- |
| 대상 계좌·시장·상품·외부 수동 거래 | 사용자 운용 범위 + provider 실제 capability 조사 | 자료 조회/모의만. 해당 live 자동 실행 불가 |
| 목표 기간·손실·집중·현금·회전율·비용 한도 | 사용자의 자금 위험 선택. 숫자 추정 금지 | 후보 비교 가능, actionable 자동 배분/증액 불가 |
| benchmark·성과 기준·세금/FX/정산 | 비교 정책 선택 + 계좌/자료 사실 확인 | 비교 불가를 표시, 우월성·최종 순수익 확정 금지 |
| 자료 사용권·시점·보존 | 공식 계약/원문·실제 관찰로 확인 | 해당 입력의 연구/재배포/실전 자격 제한 |
| 연구·외부 AI 예산·빈도 | 비용·자원 상한 선택 + 실측 | 승인 없는 유료 실행·무제한 작업 불가 |
| 원격 필요·IdP·secret·알림 수신 | 운영 환경/비용 선택 + 실제 연동 확인 | 로컬 profile 유지. 외부 공개/발송 시작하지 않음 |
| SLO/RPO/RTO·backup 위치·재활성 책임 | 운영 관찰 + 감당할 중단·손실·비용 선택 | 무인/실전 production gate 미완료 |

I1의 자료·관측 계약과 I2의 추천 조회 설계는 위 미정 숫자를 만들지 않고 구현 계획으로 전환할 수 있다. 자동 실행·증액·유료 실행·배포는 필요한 입력과 해당 gate가 갖춰진 범위만 진행한다. 이 문서는 특정 투자 전략이 수익을 낸다는 승인이나 실전 자금 투입 권고가 아니다.

## 부록 A. 근거와 정본 관계

### A.1 직접 확인한 소스

경로·위치는 기준 revision `a2eba6f`에 대한 것이다. 새 구현 이후 line은 바뀔 수 있다. PR #81의 기존 테스트/CI·main 반영과 이번 설계의 검증을 구분한다.

| 근거 | 확인한 의미 |
| --- | --- |
| [README](../../README.md#purpose) | 전체 투자 운용 목표와 현재 구현·후속 목표, 실전/증액 별도 승인 |
| [PLAN](../../PLAN.md) 101–104, 121, 146행 | FX/기업행사·실제 broker/WS·unknown·risk/fencing·운영/배포의 미완료 |
| [기존 전체 리팩토링](whole-project-refactoring.md) §15 | owner/private 경계·관측/쓰기 목적·정확한 SQL 허가·scope 수명 |
| [플랫폼 확장](scalability-and-platform.md) §§1–10 | local/P2a/P2b, 권한·cache·DB/MQ·tenant·운영 gate |
| [G6](../../gates/g6-deployment.md) | 현재 배포 미활성, 단일 노드와 PostgreSQL/Kubernetes gate 구분 |
| `services/core/internal/composition/cli.go:62` | serve 기본 loopback·timeout·ListenAndServe, Shutdown 미연결 |
| `services/core/main.go` | signal context 존재. HTTP drain이 구현됐다는 뜻은 아님 |
| `services/core/internal/modules/research/contracts/feed.go:33`, `compose.go` | PublicResearch 읽기 계약과 로컬 파일 adapter |
| `services/core/internal/modules/portfolio/contracts/queries.go:10` | Inputs/InputQuery/InputsReader와 평가 조회 계약 |
| `services/core/internal/composition/observations/portfolio.go:23` | 같은 ref·transaction·cutoff의 ledger/market 묶음 |
| `services/core/internal/storage/observation/session.go:18` | 현재 허용 목적과 참조 수명/rollback/rows 정리 |
| `services/core/internal/composition/observations/owner_price.go:24` | 목적별 owner 간 연결의 기존 예 |
| `services/core/internal/modules/trading/internal/application/ports.go:14` | private UnitOfWork와 trading 소유 WriteScope |
| `services/core/internal/modules/trading/internal/domain/strategy_admission.go:48` | promotion 허용 SMA name/version |
| `services/research/omni_research/application/experiment.py` | expanding walk-forward·후보 결과·최종 holdout. 전 시도 기록 완료는 아님 |
| `services/core/internal/architecture/module_rules_test.go` 및 `modules_test.go` | 계층·owner·SQL와 위반 fixture 자동 검사 |

이 작업에서 개인 계좌·credential·배포 환경·현실 성과·팀 채팅/회사 문서는 조회하지 않았다. source에 config가 있다는 사실을 실제 인증·사용권·운영 보장의 근거로 쓰지 않는다.

### A.2 외부 1차 근거

2026-11-16 원문을 확인했다. 이 자료의 보장은 제품의 구현·실전 검증과 별개다. provider 계약은 실제 구현 시 다시 확인한다.

- [키움 주문체결 조회 kt00009](https://openapi.kiwoom.com/m/guide/apiguide/08/kt00009): endpoint별 조회·연속조회·quota.
- [The Probability of Backtest Overfitting](https://www.davidhbailey.com/dhbpapers/backtest-prob.pdf): 여러 backtest 중 선택한 결과의 과적합 문제. 성과 보증이 아니다.
- [SQLite Atomic Commit](https://sqlite.org/atomiccommit.html), [WAL](https://sqlite.org/wal.html): DB 내부 보장·저장/다중 파일 제한과 외부 효과의 경계.
- [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html): 향후 원격 인증 profile의 보안 기준. 현재 인증 구현의 증거는 아니다.

## 부록 B. 결정·시나리오 추적

### B.1 정책 근거와 제안의 상태

대화 전체를 복제하지 않고 확인 가능한 목표만 기록한다. `U`는 사용자의 명시적 목표, `D`는 이 문서의 기술 제안이다. `D`는 사용자가 세부 정책까지 승인한 기록이 아니다. 미확정 숫자는 §14의 gate로 남긴다.

| 고정 ID | 상태·근거 | 의미와 연결 |
| --- | --- | --- |
| KAI-U01 | 확정 목표 · 수집/추천/자동 투자/리밸런싱/복기를 원한 사용자 요청과 README 목적 | 전체 흐름 보존. §1–7, I1–I6 |
| KAI-U02 | 확정 목표 · 제품 기능/차별성/효율과 투자 전략/수익성/복기 자동 개선을 함께 다룬 최신 요청 | 두 개선 흐름. §7–8, B01–B04/B16 |
| KAI-U03 | 확정 범위 · 최신 “설계부터 해줘” | 이번에는 설계·검토·문서. 신규 기능 미구현 |
| KAI-U04 | 확정 구조 · 기존 TDD·DDD·클린/모듈형·공개 계약·안쪽 의존 요청 | §3, §12, B05/B06 |
| KAI-U05 | 기존 목표 · README의 실전 활성화/증액 별도 승인 | 추천/연구 결과가 실전 권한을 만들지 않음. §5–7, B07/B15 |
| KAI-D01 | 제안 · §13 A/B 비교 | advisory 소유권·불변 handoff, §2–3, B01/B05/B07 |
| KAI-D02 | 제안 · 기존 observation 보존+실전 공백 보완 | 같은 DB의 목적별 admission/result apply. §6, B06/B08–B12 |
| KAI-D03 | 제안 · 시간/정정·선택 편향 위험 | point-in-time 근거·전체 trial·독립 검증. §4/7, B02/B03/B13 |
| KAI-D04 | 제안 · broker 외부 효과·lease 한계 | unknown 보존·철회 commit 순서·단일 writer 인계. §6, B08–B11/B14 |
| KAI-D05 | 제안 · 개인 운영+기존 플랫폼 gate | 로컬 우선·계측 후 확장·제한된 작업. §8–9, B14–B19 |
| KAI-D06 | 제안 · legacy/global 범위 보존 | 별도 namespace·writer fence·상호 배타·이전. §11, B20/B21 |
| KAI-D07 | 미확정 정책 입력 · §14 | 계좌/자금 위험·비용·성과/복구 목표. 없으면 해당 gate 차단. B04/B15/B18 |

기존 전체 리팩토링/플랫폼 결정은 유효하다. D02는 제한된 새 목적을 제안하는 후속 관계이며 기존 목적을 폐기하지 않는다. 일반 plugin host·다중 tenant·다중 host live failover는 보류다. 구현과 정책의 차이가 발견되면 이 표와 해당 검증을 갱신하며 정책을 코드에 맞춰 임의 변경하지 않는다.

### B.2 필수 시나리오

아래는 설계에서 도출한 검증 계획이다. 실제 test 파일·실행 결과는 아직 없다. 모든 주체는 개인 workspace의 권한 있는 사용자/등록 service를 기본으로 하며, 다른 계좌·환경·mode는 별도 scope다. 같은 내용의 결과를 이유 없이 여러 계층에서 반복하지 않는다.

| ID / 결정 | 사전 상태·행동 | 기대 결과·보존/금지 | 검증 계층 / 단위 |
| --- | --- | --- | --- |
| B01 / U01,U02,D01 | 충분한 근거로 추천을 생성하고 사용자가 보류 | 근거/hash·유지 대안·보류 이유 보존. 승인/주문 생성 금지 | domain+주요 E2E / I2 |
| B02 / D03 | USD 가격 또는 FX 누락·stale인 KRW/USD 평가 | 원통화·누락 이유 유지. 근거 없는 통합 합계·actionable 계획 금지 | API+실제 관측 DB / I1 |
| B03 / D01,D03 | 추천 후 가격/연구 근거 철회·정정, block 중단·dispatch 경합, 과거 보고서 재조회 | 당시 version 유지·block 우선 commit·무효/대체 연결. pending을 철회 완료로 표시하거나 stale artifact로 신규 dispatch 금지 | 같은 Tx 자격·DB+API / I1,I2,I6 |
| B04 / U02,D07 | 비용/세금/benchmark 조건 불일치 또는 위험 입력 미정 | 비교 불가/입력 필요. 우월성 확정·무한 자동 최적화 금지 | domain / I3 |
| B05 / U04,D01 | 타 owner SQL/internal import·순환을 의도적으로 추가 | architecture gate가 거절. 정식 public contract만 허용 | 정적 위반 fixture / 각 단위 |
| B06 / U04,D02 | 다른 DB/목적/종료/위조 ref로 admission 또는 fill apply | 모든 접근 거절·쓰기 없음. rows/connection 정리 | 실제 SQLite·수명/role / I6 |
| B07 / U05,D01 | 추천 채택 또는 다른 계좌 권한의 사용자/worker가 승인 요청 | 채택 기록만 저장 또는 권한 거절. receipt/계좌 정보 유출·권한 생성 금지 | API/권한 통합 / I2,I4 |
| B08 / D02,D04 | 두 요청이 같은 자금, 같은 plan/approval과 같은/다른 command ID로 경합 | 같은 키 수렴·입력 충돌, 승인 한 번 소비·plan당 실행 하나·leg당 intent 하나, 전체 예약 한도 유지 | 별도 SQLite 연결·race / I4,I6 |
| B09 / D04 | dispatch commit 직전/직후 revoke를 경합 | revoke 먼저면 거절, dispatch 먼저면 in-flight. 소급 취소 완료 주장 금지 | 실제 DB barrier / I6 |
| B10 / D04 | dispatch commit 후 전송 전/응답 후 process 사망 | unknown/receipt 보존·대조. blind 재전송·새 키 중복 주문 금지 | 별도 process+provider harness / I6 |
| B11 / D02,D04 | 부분/중복/역순 fill과 cancel 후 late fill, 같은 ID 다른 내용 | 사실은 한 번 반영·충돌 격리·예약 재대조. 체결 버리기/조기 해제 금지 | 실제 DB+provider contract / I4,I6 |
| B12 / D02 | ledger 반영 또는 trading receipt 저장 직전에 오류 | 같은 result transaction 전부 rollback. 외부 체결은 pending으로 재조회/재적용 | 실제 DB 실패 주입 / I6 |
| B13 / D03 | holdout을 본 뒤 ID를 바꿔 재탐색하거나 중단 후보 삭제 시도 | family 이력·실패 보존, 독립 검증/승격 거절 | Python+Go 계약 / I3 |
| B14 / D04,D05 | 옛 writer/host 종료 불명 상태에서 lease 만료·새 process 시작 | 새 live writer 차단. DB fence로 broker 차단 완료 주장 금지 | process/인계 통합 / I5,I6 |
| B15 / U05,D07 | 좋은 paper 결과·연구 설치·새 한도로 자동 활성화 시도 | 기존 권한 유지, live/증액 거절. 별도 승인 필요 표시 | API/authority 통합 / I3,I6 |
| B16 / U02,D05 | 복기에서 개선 후보 생성·제품 변경안 생성 | 제한된 연구/변경안만. 운용 안전 정책 수정·자동 배포 금지 | job/권한 계약 / I3 |
| B17 / D05 | 연구 budget 초과·cancel·lease 만료, 옛 attempt 늦은 결과 | 새 작업 차단·소유 자식 종료/대기·현재 attempt 보호·부분 근거 보존 | 실제 process+DB / I3,I5 |
| B18 / D05,D07 | backup 이후 체결 존재, 복원·재시작 | 비활성 시작·이전 writer 차단·broker 대조·재승인. 과거 armed 부활 금지 | restore+process / I5,I6 |
| B19 / D05 | 인증/TLS 없는 remote, unsupported provider/mode, queue/disk 압박 | 기동/신규 위험 차단, 결과 기록 우선. cache 허용·무한 적재 금지 | profile+실제 자원 통합 / I5,I6 |
| B20 / D06 | legacy SMA와 새 rebalancer가 같은 계좌를 동시에 활성화 | 하나의 실행 regime/예약 권위. 두 runner 독립 자금 사용 금지 | migration+SQLite 경합 / I4 |
| B21 / D06 | backfill 중 최신 정정·새 writer 쓰기 후 old binary 기동 | 최신 revision 보존·구 writer 거절·정합성 뒤 cutover | 실제 migration/호환 / I1,I4 |
| B22 / U01,D04 | 매도 일부만 체결, 매수 자금 부족/다른 통화 | 필요한 leg 대기·잔여 표시·새 계획 필요. 예상 대금/자동 FX/반대 보상 주문 금지 | domain+paper DB / I4 |
| B23 / D03,D05 | 수집 페이지 commit 전후 실패·WS gap·quota·정정 중복 | cursor와 자료 함께 복구·gap/freshness 표시·endpoint budget 준수 | DB+수집 adapter / I1 |
| B24 / U01,D01 | 실행 중 앱 연결 끊김·재접속·같은 키 조회 | 저장 상태/partial/unknown 표시·키보드 접근. 재접속이 신규 주문을 만들지 않음 | API+E2E / I4,I5 |
| B25 / U01,U05,D02 | 같은 mandate 회차 동시 기동·앞 회차 partial/unknown·기간 비용 상한 도달 | 회차 수렴·미해결 우회/중복 계획 금지·한도 차단. 새 계획은 현재 권한/누적 사용량 검증 | 실제 SQLite·runner/authority / I4,I6 |

시나리오→결정뿐 아니라 각 유효 목표/제안→시나리오도 대조한다. 구현 시 이 ID를 테스트 이름·metadata 또는 기존 검증 표에 연결한다. 지원하는 provider/profile별 통합 범위를 기록한다. 시나리오 작성만으로 기능·실제 인증·운영 확인을 완료 처리하지 않는다.

## 부록 C. 구현 영향 지도

| 단위 | 현재 경로 / 신규 제안 경로 | 함께 바꿀 호출자·검사·문서 |
| --- | --- | --- |
| I1 | `modules/marketdata/`, `modules/portfolio/contracts/queries.go`, `composition/observations/portfolio.go` | source adapter/cursor·owner proof·신규 evidence schema·client 조회·관측/정정 fixture |
| I2 | **신규** `services/core/internal/modules/advisory/{contracts,internal}/`, `compose.go` | `composition/http.go`·`resources.go` 배선, architecture owner/DAG/SQL grants·maintenance proof·**신규** client advisory feature |
| I3 | `services/core/internal/modules/research/`, `services/research/omni_research/{domain,application,adapters}/` | **신규** study/trial artifact schema·cross-language fixtures, client review, promotion admission 회귀 |
| I4 | `services/core/internal/modules/trading/{contracts,internal}/`, `services/core/migrations/` | **신규** rebalance 계약/계획·scope migration·legacy runner exclusion·client 승인/실행·SMA 기존 fixture |
| I5 | `services/core/internal/composition/cli.go`, `composition/resources.go`, `modules/maintenance/` | process supervisor·readiness/알림 adapter·restore inactive·수명 통합 검사·실제 runbook |
| I6 | `internal/storage/{contracts,observation,ownership}/`, `composition/observations/`, ledger/marketdata/research/trading private adapters | **신규** 목적별 scoped 계약·source eligibility·SQL/ref 부정 fixture·동일 Tx failure/race·provider 실제 관찰 gate |
| 공통 | `contracts/openapi.json`, `contracts/*.schema.json`, `apps/client/lib/features/`, 기존 `tests/`·architecture 검사 | 각 수직 단위의 producer/consumer/음성 fixture·PLAN/GATES·관련 owner 설계/보안/운영 문서 |

`modules/...` 축약 경로는 `services/core/internal/modules/...` 아래다. 위 경로는 소유권 지도이며 전부 새 파일을 만들라는 명령이 아니다. 필요한 기존 구현을 재사용하고 변경한 업무 결과를 검증하는 최소 파일만 추가한다.

| 요구 → 설계 | 결정 | 시나리오 | 구현 단위·gate |
| --- | --- | --- | --- |
| 정보·추천·투자·리밸런싱·복기 | U01,D01,D02 | B01–B03,B07–B12,B22,B24,B25 | I1–I6, 실제 실행은 별도 활성화 |
| 기능·차별성·효율과 투자 자동 개선 | U02,D03,D05 | B04,B13,B16,B17,B23 | I2,I3,I5, 비교/예산·미관측 검증 |
| DDD·클린·모듈형·데이터 소유권 | U04,D01,D02 | B05,B06,B08,B12 | 각 owner 구조·실제 DB 검사 |
| 새 DB·공급자·MQ·복수 인스턴스·MSA 가능성 | U04,D05,D06 | B05,B14,B19–B21 | §8/11, 지원 profile에만 실제 backend suite |
| 안전한 승인·재시도·복원 | U05,D02,D04,D07 | B07–B12,B14,B15,B18 | I4–I6, unknown·writer·복구 gate |
| 설계 먼저·상태 구분·TDD·정리 | U03,U04,D05 | §12와 각 시나리오의 계층 | 문서 검토 후 구현, 최종 소스 검증·소유 자원 정리 |

이 문서가 확정하지 않은 입력은 §14와 D07에 남긴다. 목표나 위험을 줄여 완료로 보이게 하지 않으며, 승인된 독립 범위와 입력 대기 범위를 나눠 다음 작업을 시작한다.
