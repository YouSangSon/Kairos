# Kairos 서비스·데이터·플랫폼 확장 설계

- 기준일: 2026-11-05. 상태: 목표 설계. DB migration·인증·캐시·MQ·MSA는 이 문서 작성으로 구현되거나 활성화되지 않는다.
- 범위: 여러 사용자·계좌·전략, 서비스 독립 확장, 여러 DB, 인증·권한, 캐시, Kafka/RabbitMQ 단독 및 혼합 구성.
- 연결: [전체 리팩토링 설계](whole-project-refactoring.md), [ADR-0001](../adr/0001-runtime-and-monorepo.md), [SECURITY](../../SECURITY.md).

## 1. 결정: 업무의 보장을 고정하고 인프라는 역할별로 선택한다

현재 단일 사용자·loopback·SQLite 프로필을 보존하면서, 다음 단계에서 서비스를 추출할 수 있는 경계를 만든다. 특정 DB나 MQ를 전체 제품의 필수 기술로 고정하지 않는다. 그렇다고 모든 DB를 `Repository<T>` 하나로, 모든 메시징을 `Send(any)` 하나로 숨기지도 않는다. 원장 transaction, 시계열 조회, 재생 가능한 event log, 작업 queue는 서로 다른 보장과 포트를 갖는다.

동일 상태에는 하나의 권위 있는 writer가 있다. 여러 DB의 사용은 업무별 소유 데이터 또는 재구축 가능한 projection을 뜻하며, 같은 원장을 두 DB에 동시에 쓰고 둘 다 정답으로 취급하지 않는다. Kafka와 RabbitMQ를 함께 쓰더라도 어느 메시지가 사실이고 어느 메시지가 명령인지, 어떤 서비스가 실행하는지 한 곳에서 결정한다.

| 단계 | 배포·저장 형태 | 진입 조건 |
| --- | --- | --- |
| P0 로컬 | 기존 Go 모놀리스 + SQLite + Python CLI + Flutter | 현재 기준. 새 외부 노출 없음 |
| P1 경계 완성 | 순수 domain·application·adapter, 명시적인 상태 owner | 전체 refactor 검증. 네트워크 호출을 흉내 내는 내부 RPC 없음 |
| P2a 단일 서버 | 단일 Go writer + local/RWO SQLite + OIDC/TLS/secret manager | 원격 접근만 필요할 때. 단일 replica·같은 host 파일·backup/restore gate 유지 |
| P2b 다중 프로세스 서버 | PostgreSQL 계열 authority + OIDC + stateless API + worker 역할 | 두 번째 replica, 독립 DB writer/worker, HA/PITR 요구. migration·권한·복구 검증 선행 |
| P3 선택적 분리 | 수집·연구·projection 등 별도 서비스 + 필요한 DB/MQ | 독립 부하·장애·배포 요구를 측정하고 소유권·event 계약·운영 owner 확보 |
| P4 복합 운영 | Kafka/RabbitMQ 병행, 분석/검색 store, 필요 시 Kubernetes | 단일 역할로 충족 못 하는 요구와 운영 여력, 재처리·DR·부하 검증 |

P2a~P4는 계획이며 즉시 설치할 구성 목록이 아니다. 필요 조건이 없으면 P1에 머물러도 설계 목적을 만족한다. 프로필을 바꾸는 일은 schema/보안/운영 변경이므로 별도 구현 단위로 진행한다.

## 2. 제품 흐름과 서비스 추출 경계

수집 → 정규화·출처 확인 → 기회/전략 평가 → 추천 또는 승인된 목표 비중 → 위험 확인 → 주문·체결 확인 → 원장 반영 → 성과·복기라는 업무 흐름을 유지한다. 추천, 리밸런싱 계획, 실행 명령, 브로커 체결 사실은 서로 다른 객체다. 연구 산출물이나 LLM 출력이 바로 주문이 되지 않는다. 미래 recommendation/rebalancing 모듈은 근거·전략 버전·가격 기준 시각·만료·사용자 정책을 담은 제안만 내고 execution이 실행 시점에 다시 검증한다. 아직 없는 기능은 refactor 완료로 계산하지 않는다.

| 추출 단위 | 소유 데이터 | 외부 제공 | 일관성·분리 조건 |
| --- | --- | --- | --- |
| 시장 수집/정보 정규화 | provider cursor, raw reference, 정규화 관찰 | observation event, 조회 API | 중복 수집 허용·식별자 dedup, provider별 quota 격리. 우선 추출 가능 |
| 연구/백테스트 | 입력 snapshot, 실험·artifact·평가 | immutable artifact와 완료 event | operational DB/주문 credential 접근 금지. CPU/배치 독립 확장 |
| portfolio projection/검색/분석 | 원장·관찰에서 파생한 read model | revision/as_of/lag가 있는 조회 | eventual consistency 허용. 명령 승인에는 사용 금지 |
| 알림 | delivery intent, 수신 설정, 전송 결과 | 알림 상태 | 투자 기록과 분리. 알림 실패가 주문 재실행을 유발하지 않음 |
| identity/access | 사용자·소속과 접근 관리 요청 | 인증된 actor, membership, trading grant 변경 요청 | trading grant/mandate의 최종 상태·epoch는 authority DB 소유. IdP disable과 거래 권한 철회 완료를 구분 |
| trading authority | ledger 원장, 주문, 예약, authority, 선택, paper 회계·runner·승격 | 상태 조회·검증된 명령·commit 이후 사실 event | 현재 교차 transaction을 한 서비스/authority DB 안에 유지 |

ledger와 execution은 논리 모듈로 분리하되 현재의 회계·예약·fill 원자성이 유지되는 동안 같은 배포/DB 경계에 둔다. 서비스마다 DB를 하나씩 주기 위해 이 원자성을 분산 transaction으로 바꾸지 않는다. 이후 분리가 필요하면 회계 전달·예약·보상·미해결 상태를 별도 설계해야 하며, 이미 체결된 거래를 saga rollback으로 취소할 수 있다고 가정하지 않는다.

서비스 추출 후 직접 테이블 join/write는 금지한다. 필요한 읽기는 소유자 API 또는 versioned event로 만든 projection을 사용한다. domain 객체를 그대로 네트워크로 serialize하지 않고 별도 integration DTO를 쓴다. 동기 호출에는 deadline·동시성 제한·오류 종류를, 비동기 호출에는 저장·재시도·순서 계약을 둔다. gRPC는 기본 도입하지 않으며 기존 HTTP/JSON 계약과 필요 시 이벤트로 시작한다.

## 3. 여러 DB를 사용하는 방법

| 역할 | 현재/후보 | 업무가 요구하는 보장 | 사용 금지 |
| --- | --- | --- | --- |
| 거래 authority | 현재 SQLite, 서버 프로필 PostgreSQL 우선 | transaction, unique/CAS, durability, 정확한 금액, fence | cache/검색 DB를 원장으로 사용 |
| 관찰 시계열 | 초기 authority DB의 별도 owner; 규모 증가 시 PostgreSQL 시계열/columnar 후보 | source+instrument+time 식별, range query, late correction/version | 마지막 값만 남겨 연구 입력 provenance 삭제 |
| 분석 projection | columnar DB 후보 | append/집계·rebuild·watermark | 실행 위험 한도 승인 |
| 공개 정보/검색 | document/search DB 후보 | 원문 reference, 색인 버전, 삭제·재색인 | 검색 index를 원문 authority로 간주 |
| 큰 raw/artifact | 로컬 private files → object storage 후보 | content hash, immutable version, private access, retention | raw credential/불필요한 계좌정보 포함 |
| 임시 cache | process memory → Redis 호환 후보 | bounded TTL·격리·eviction 허용 | 유일한 주문/lease/idempotency 저장소 |

후보 제품은 채택된 의존성이 아니다. workload·백업·라이선스·운영 비용·정합성 요구로 좁힌다. 필요한 adapter 하나와 contract suite를 구현한 뒤 추가한다. 실제 두 번째 구현이 없을 때 generic SQL dialect engine이나 동적 plugin loader를 만들지 않는다.

각 DB adapter는 다음 capability를 증명한다: transaction 격리, commit 오류의 의미, unique 충돌, CAS 영향 행 수, UTC/정밀도, event 순서, 취소·pool 고갈, restore. SQL driver만 교체해 같은 의미가 된다고 가정하지 않는다. authority UnitOfWork는 한 DB에만 적용한다. 분리된 두 DB를 한 callback으로 묶는 API를 금지한다.

### PostgreSQL 승격

첫 PostgreSQL 전환에서는 모든 authority write가 DB 전체의 동일한 transaction-scoped 잠금을 먼저 얻어 기존 단일 writer 순서를 보존한다. 현재 `order_events.sequence`의 RISK_APPROVED/SUBMIT_DISPATCHED 연속 쌍, ledger revision, strategy selection, watchlist singleton과 policy의 전체 armed-account 범위는 계좌별 상태가 아니다. 계좌 잠금만 추가해 전역 순서를 깨뜨리면 안 된다. event sequence를 쓰는 모든 writer가 같은 잠금 규칙을 따라야 하며, pair 두 insert 사이에 다른 event writer가 끼지 않는 테스트가 필요하다.

이 단계의 확장 이익은 다중 API 연결·읽기·배치 격리와 운영 복구이며 authority write의 무제한 병렬화가 아니다. 이후 쓰기 병렬화는 global singleton을 tenant/account/strategy scope로 바꾸는 별도 schema·proof 버전 migration이다. 그때 global metadata → tenant policy → account → aggregate 순서로 필요한 row를 잠그고, 여러 계좌의 상위 한도와 selection/policy가 공유하는 scope도 포함한다. phantom/write-skew는 SERIALIZABLE 또는 동등한 잠금으로 검증한다. 첫 PostgreSQL 승격도 단일 tenant를 유지한다. tenant 추가만으로 현재 전역 정책을 tenant별 정책으로 몰래 바꾸지 않는다.

serialization failure는 결정 로직을 포함한 전체 DB transaction을 다시 수행해야 한다. 이 재시도는 외부 부작용이 없는 DB 단계에만 제한하고 deadline/횟수 상한·같은 command ID를 유지한다. commit 결과 자체가 불명확하면 먼저 command receipt를 조회한다. broker 호출을 transaction retry callback에 넣지 않는다. [PostgreSQL 격리 문서](https://www.postgresql.org/docs/current/transaction-iso.html)

금액은 기존 canonical decimal/raw bytes와 exact 계산을 보존한다. PostgreSQL NUMERIC을 쓰더라도 임의 fixed scale로 반올림하지 않는다. 무한소수 유리수·중간 계산은 domain exact가 소유하며 DB로 계산 정책을 옮기지 않는다. 기존 해시 대상 payload는 원래 bytes를 보존한다.

migration 순서: 신규 명령 차단 → writer/runner 정상 drain 및 lease 상태 기록 → 검증된 backup/restore rehearsal → 목표 schema 생성·변환 → 행 수/키/금액/이벤트 순서/hash/재생 결과 비교 → 새 authority 단독 시작 → 읽기·reconciliation 확인 → 쓰기 개방. dual-write는 하지 않는다. 첫 새 쓰기 전에는 원본으로 복귀 가능하다. **새 DB에 쓰기가 발생한 뒤에는 구 DB를 바로 다시 켜지 않는다.** 새 로그를 보존한 역이전 또는 forward fix를 검증해야 한다.

read replica는 읽기 전용 projection에만 사용한다. 쓰기 직후 읽기는 primary 또는 최소 revision 충족 경로로 보낸다. 계좌·권한·fence·kill switch의 판정은 stale replica에서 하지 않는다. pool 크기는 replica 수×connection 수 합계가 DB 한도를 넘지 않게 제한한다.

### tenant와 복수 계좌

미래 서버 프로필의 `tenant_id`는 사용자/가구/조직의 데이터 격리 단위이며 현재 단일 owner와 다르다. 사용자 ID, tenant ID, broker account alias를 구분한다. tenant는 요청 body를 신뢰하지 않고 인증 주체와 membership에서 결정한다. child FK와 unique/idempotency/cache 키에도 tenant를 포함한다. 공용 시세와 개인 포트폴리오는 별도 scope다.

기존 로컬 데이터는 명시적인 단일 tenant로 backfill하되 원장/event canonical bytes를 수정해 과거 hash를 깨뜨리지 않는다. tenant mapping을 외부 metadata로 추가하고 기존 ID/hash 연결을 검증한다. 누락 row, 교차 tenant FK와 ownership 미확정 데이터가 있으면 다중 사용자 개방을 막는다. 현재 global ledger revision·strategy selection·runner scope·watchlist·policy scope를 tenant별로 나누는 schema 및 recovery-proof migration도 다중 tenant 개방의 선행 gate다. 별도 tenant metadata만 붙인 상태는 복수 tenant를 지원하지 않는다.

PostgreSQL RLS는 추가 방어로 쓸 수 있다. application의 계좌별 권한 검사를 대체하지 않는다. pool checkout마다 transaction-local tenant context를 설정하고 rollback/return 후 누출되지 않는지 검사한다. 서비스 role에 BYPASSRLS/owner 권한을 주지 않는 구성을 검증한다. [PostgreSQL RLS](https://www.postgresql.org/docs/current/ddl-rowsecurity.html)

## 4. 인증, 권한, 비밀의 분리

현재 `SECURITY.md`의 인증/TLS 없는 loopback 프로필은 그대로이며 공개 서버로 사용할 수 없다. remote profile은 인증·TLS·권한 gate를 통과하기 전 시작을 거부하도록 구현한다. 기존 local profile과 별도 명시 설정을 쓰고 주소 변경만으로 remote 권한이 생기지 않게 한다.

인증은 검증된 OIDC 공급자에 맡기는 방향이다. 직접 password store나 token 발급 서버를 만들지 않는다. 구체 IdP 제품은 배포 환경 결정 시 선택한다. native Flutter는 Authorization Code + PKCE와 OS 보안 저장소를 사용한다. web은 같은 origin의 BFF/session cookie를 우선 설계하고 HttpOnly/Secure/SameSite와 CSRF 검증을 적용한다. implicit/password grant를 쓰지 않는다. redirect URI, issuer/audience, signature algorithm, expiry, nonce/state를 검증한다. refresh rotation/폐기와 계정 탈퇴를 session 정책에 포함한다. [OAuth 보안 BCP](https://www.rfc-editor.org/rfc/rfc9700.html)

| 권한 | 허용 범위 | 추가 제한 |
| --- | --- | --- |
| viewer | 소유 scope의 읽기 | alias라도 다른 계좌 ID 열람 금지 |
| researcher | 연구 실행·보고서 생성 | operational write·broker credential 없음 |
| operator | 허용된 import, paper 운영 | live 승인 권한과 분리 |
| owner | 계좌 grant, 한도·승인 정책 관리 | 중요 변경 시 재인증/MFA 및 audit |
| service principal | 지정 service/API/event 소비 | 사람 token 전달 대신 audience·scope 제한 |

역할은 큰 범위만 정한다. 각 명령은 actor, tenant membership, account grant, action, mode, 만료와 정책 revision을 확인한다. 인증 성공이 투자 승인이나 broker 권한을 의미하지 않는다. 자동 실행은 단기 user access token에 매달리지 않고 별도로 저장한 만료 있는 mandate를 참조하며, 매 dispatch에서 revocation·limit·allowlist·kill switch·fence를 다시 확인한다. 이 live mandate는 향후 gate이며 이번에 활성화하지 않는다.

포트는 `Authorize(actor, action, resource, policyRevision)`처럼 업무 자원을 받는다. JWT/OIDC 타입은 auth adapter가 처리하고 application에는 검증된 principal을 전달한다. CLI의 local owner와 서비스 identity도 명시적인 adapter를 거친다. 거래에 사용되는 grant/mandate, revocation epoch, policy revision은 trading authority DB가 최종 소유하고 dispatch transaction과 같은 잠금 범위에서 검사한다. identity 서비스의 fresh RPC만 호출한 뒤 dispatch하는 방식은 금지한다. RPC 직후 철회가 일어날 수 있기 때문이다.

철회는 authority에 idempotent revoke command를 보내고 그 DB의 epoch 증가·halt commit 뒤에만 **거래 권한 철회 완료**로 응답한다. 네트워크 장애로 commit을 확인하지 못하면 pending/unknown으로 표시하고 같은 command ID의 receipt를 조회한다. identity의 요청 접수나 IdP 사용자 disable만으로 trading revoke 완료라고 표시하지 않는다. revoke가 먼저 commit되면 이전 epoch의 dispatch를 거절하고, dispatch가 먼저 commit됐다면 이미 허가된 in-flight 주문으로 분류하여 취소 가능성 확인/reconciliation을 별도로 수행한다. 철회가 이미 제출된 주문을 소급 취소한다고 약속하지 않는다. 읽기 권한 철회는 access policy가 별도로 소유하며 trading revoke receipt와 혼동하지 않는다. JWT에 오래 남은 role claim만으로 거래하지 않는다.

서비스 간 통신은 TLS와 audience 제한된 workload identity를 사용한다. gateway가 있어도 각 서비스가 resource 권한을 검증한다. broker secret은 execution/해당 provider adapter만 받으며 research·Flutter·event·log·cache로 전달하지 않는다. 로컬 keychain에서 서버 secret manager로 이전할 때 키 version·rotation·lease와 감사 trail을 둔다. revoke/rotate 작업 자체는 별도 운영 승인 사항이다.

IdP 장애 시 이미 검증 가능한 만료 전 token의 제한된 읽기는 정책 범위에서 가능하지만 새 login/refresh는 실패한다. 검증 키를 찾지 못하거나 권한 freshness를 확인할 수 없으면 허용으로 fallback하지 않는다. kill switch와 revoke는 cache/MQ가 중단되어도 authority DB 경로로 작동해야 한다.

## 5. 캐시: 버려도 정답을 잃지 않는 데이터만

초기는 process-local bounded cache로 충분한 지점을 측정하고 replica 간 공유가 필요한 경우 Redis 등을 추가한다. client read cache, server cache, DB projection을 구분한다. cache 장애를 복구하려고 잘못된 주문 상태를 만들어서는 안 된다.

| 대상 | key·검증 | stale/장애 정책 |
| --- | --- | --- |
| 공개 가격·차트 | provider+instrument+interval+adjustment+range+version | as_of/freshness와 함께 읽기 표시 가능. execution은 허용 신선도를 다시 검증 |
| portfolio projection | tenant+account+ledger_revision+가격/FX 입력 버전+계산 버전 | 필요한 revision 미달이면 primary 계산/명시 pending. 새 값인 것처럼 표시 금지 |
| 공개 immutable research artifact | content hash+schema version | 검증된 immutable bytes만, private artifact와 keyspace/ACL 분리 |
| 권한 표시용 정보 | tenant+subject+policy revision+짧은 TTL | 실행 권한의 정답으로 사용 금지 |
| 주문 idempotency/예약/fence/kill switch | authority DB 소유 | cache-only 저장 금지 |

권한 검사를 cache lookup 전에 하고 key에 scope를 넣는다. revoke 후 이전 private 응답을 반환하지 않도록 policy revision/권한 검사를 함께 적용한다. TTL만으로 tenant 격리를 구현하지 않는다. cache-aside 읽기와 commit 이후 invalidation event를 사용하되 invalidation 유실을 전제로 revision key와 TTL로 수렴시킨다. version 없는 오래된 fill이 최신 값을 덮지 못하게 generation/CAS를 적용한다.

stampede는 요청 합치기(singleflight), bounded 동시성, TTL jitter로 제한한다. Redis 장애 시 읽기는 DB 부하 예산 내에서 우회하고 초과분은 명시적으로 제한한다. 무제한 DB fallback을 만들지 않는다. 새 cache adapter에는 hit/miss/age/eviction, cross-tenant, invalidation 유실, reconnect, stale refill 검증이 필요하다.

## 6. 메시지의 종류와 여러 broker의 공존

| 종류 | 의미 | 포트 | 예시 |
| --- | --- | --- | --- |
| integration event | 이미 commit된 사실, 여러 소비자 가능 | `EventPublisher.Publish` / subscription adapter; replay는 별도 `ReplaySource` | observation recorded, ledger revision advanced, experiment completed |
| work command | 한 owner가 수행할 요청, 거부·만료 가능 | `JobQueue.Enqueue` / worker adapter | backtest 실행, 수집 job, 보고서 생성 |
| process-local domain event | 한 transaction 안의 조합 신호 | 직접 함수/도메인 결과 | 승인 판단과 상태 전이 |

`EventPublisher`, `ReplaySource`, `JobQueue`를 같은 인터페이스로 합치지 않는다. RabbitMQ의 일반 queue를 Kafka replay log처럼 취급하지 않도록 publication과 replay도 분리한다. broker는 integration adapter이며 domain이 topic/partition/delivery tag를 알지 않는다. application 명령은 `job_id`와 상태/결과 조회를 제공하고 broker ACK를 업무 성공으로 반환하지 않는다. synchronous API도 command ID와 재조회 가능한 receipt를 유지한다.

| 구성 | 적합한 용도 | 보완할 계약 |
| --- | --- | --- |
| DB outbox + poller | 초기 낮은 처리량, 외부 broker 없는 비동기 작업 | durable claim/lease, bounded backlog와 재시도 |
| Kafka 중심 | 보관·재생되는 사실 stream, 다수 독립 projection | aggregate key/sequence, consumer offset, retention와 rebuild |
| RabbitMQ 중심 | routing이 필요한 작업 queue, worker 분배 | publisher confirm + consumer manual ACK, durable queue, redelivery/dedup |
| Kafka + RabbitMQ | 사실 stream과 작업 dispatch가 각각 필요한 경우 | 명시적 bridge owner, 하나의 job authority, 두 시스템 간 원자성 없음 |
| 다른 broker | 동일 역할의 구현 후보 | 그 역할의 capability suite 통과. 이름만 바꿔 호환이라고 하지 않음 |

각 단독/혼합 profile은 아래처럼 모든 역할을 채운다. 표는 허용 가능한 목표 조합이며 현재 설치·지원 목록이 아니다.

| 프로필 | 사실 발행/구독 | 과거 사실 replay source | 작업 전달·claim | 권위 DB |
| --- | --- | --- | --- | --- |
| broker 없음 | DB outbox poller → 각 소비자 | owner event archive + watermark | DB job poller/lease | 해당 업무 DB |
| Kafka만 사용 | outbox relay → Kafka topic/consumer group | Kafka retention 내 log, 이후 owner archive | 우선 DB job poller/lease. 향후 Kafka command topic은 별도 JobQueue adapter suite 필요 | 해당 업무 DB; Kafka가 job/주문 상태를 대체하지 않음 |
| RabbitMQ만 사용 | outbox relay → exchange → 소비자별 durable queue | owner DB event archive의 cursor 기반 export. 일반 queue ACK 후 기록은 replay source 아님 | RabbitMQ job queue + DB claim/lease | 해당 업무 DB와 replay archive |
| Kafka + RabbitMQ | Kafka facts, bridge의 durable job intent | Kafka + owner archive | RabbitMQ jobs + DB claim/lease | 각 업무 DB |

broker 없는 poller도 소비자별 cursor/inbox가 필요하다. Rabbit-only 신규 projection은 archive snapshot/watermark를 먼저 받고 durable subscription에서 중복을 흡수하며 catch-up한다. snapshot 동안의 변경을 놓치지 않도록 subscription을 먼저 확보하거나 archive가 cutover까지의 모든 event를 보관해야 한다. `ReplaySource`가 없는 adapter는 replay 필요 profile에 연결할 수 없다.

포트의 outcome도 분리한다. `Publish(envelope)` 성공은 지정 destination의 durable 접수 확인이며 consumer 업무 완료가 아니다. `Enqueue(command)` 성공은 전달 접수이며 job 완료가 아니다. `Replay(afterCursor, limit)`는 bounded records와 다음 cursor/retention gap을 반환한다. ACK/nack·Kafka offset은 consumer adapter 내부에서 DB commit 결과에 따라 처리한다. timeout은 확정 실패와 unknown을 구분하고 동일 ID 재발행 규칙을 적용한다.

Kafka는 partition 단위 순서를 사용하며 다른 partition 사이 총순서를 가정하지 않는다. RabbitMQ publisher confirm은 broker의 접수이며 consumer 업무 완료와 별개다. delivery tag/offset은 adapter 내부에 남는다. broker의 transaction/exactly-once 기능을 DB나 외부 주문까지의 exactly-once로 확대하지 않는다. [Kafka 문서](https://kafka.apache.org/41/design/design/#message-delivery-semantics), [RabbitMQ confirms](https://www.rabbitmq.com/docs/confirms)

### 공통 envelope와 전달 원칙

목표 envelope 필드: `event_id`, `event_type`, `schema_version`, `producer`, `tenant_id`(공용이면 명시 public scope), `aggregate_type/id`, `aggregate_sequence`, `occurred_at`, `recorded_at`, `correlation_id`, `causation_id`, `payload`. command에는 별도 `command_id`, `expires_at`, 대상 account/mandate reference를 둔다. 모든 ID는 안정적으로 유지하며 broker retry에서 새로 생성하지 않는다. secret·raw account·access token은 금지하고 큰 artifact는 hash와 권한 있는 reference로 전달한다. event type별 schema와 권한 있는 producer를 검증한다.

1. owner DB transaction에서 업무 상태와 outbox를 함께 저장한다. outbox 삽입 실패면 업무 상태도 rollback한다.
2. relay는 claim/lease를 확보하고 destination별 전달 상태를 관리한다. publish confirmation 후 전달 기록을 commit한다. 중간 crash는 같은 event ID로 다시 보낸다.
3. consumer는 `(consumer_name, event_id)` unique inbox와 업무 효과를 **같은 대상 DB transaction**에 commit한 뒤 ACK/offset 진행한다.
4. DB commit 후 ACK 전 crash는 재전달되지만 inbox가 중복 효과를 막는다. broker ACK 전에 효과를 commit하지 못하면 ACK하지 않는다.
5. 외부 부작용은 inbox만으로 원자화할 수 없다. 별도 durable intent→외부 호출→outcome 패턴을 사용한다. 주문은 기존 dispatch/reconciliation 경계를 유지한다.

동일 aggregate의 모든 전이를 전달하는 stream은 연속 sequence를 검증하고 gap은 보류/재조회한다. 필터링한 event stream은 일부 revision이 정상적으로 빠질 수 있으므로 source revision과 별도의 stream sequence 또는 predecessor를 계약에 명시한다. 미래 version·잘못된 schema는 무한 retry하지 않고 quarantine으로 보낸다. 오래된 observation과 거래 event를 같은 last-write-wins 규칙으로 처리하지 않는다.

### broker별 완료 조건

Kafka consumer의 초기 구현은 **partition 안에서는 한 번에 한 record만 처리**하고 partition 간에만 병렬화한다. DB effect+inbox commit 또는 durable job handoff가 끝난 뒤 해당 record의 다음 offset을 commit한다. poll로 받은 위치를 곧바로 commit하지 않는다. 나중에 partition 내부 병렬 처리가 필요하면 수신한 record 순서의 완료된 prefix까지만 offset을 진행한다. Kafka offset 숫자가 연속이라는 뜻은 아니다. 뒤 record가 먼저 끝나도 앞의 미완료 record를 건너뛸 수 없다. rebalance 시 새 처리 차단·완료 prefix 확정·남은 작업 취소/lease fencing을 수행하며, 이전 owner가 뒤늦게 commit하는 경로를 차단한다. [Kafka consumer position과 병렬 처리](https://kafka.apache.org/41/javadoc/org/apache/kafka/clients/consumer/KafkaConsumer.html)

RabbitMQ relay는 confirm만으로 delivered를 표시하지 않는다. 필수 destination별 안정된 durable queue와 routing을 검증하고, persistent message + publisher confirm + mandatory return 처리를 함께 적용한다. `basic.return`, nack, timeout은 delivered가 아니다. mandatory는 최소 한 queue로의 routing만 알려주므로 필수 소비자가 여러 개면 destination별 outbox delivery와 전용 routing을 둔다. 명시한 queue로 보내는 직접 경로와 관리된 topology를 사용하고, 다른 queue 한 곳으로만 라우팅됐는데 전체 fanout 성공으로 간주하지 않는다. queue 삭제·binding 변경은 관리 권한과 변경 gate로 제한한다.

classic durable queue와 persistent message는 단일 node 복구 계약이고, HA가 필요하면 quorum queue의 replica/confirm 보장을 별도 profile로 검증한다. durable queue라는 이유만으로 transient 메시지의 재시작 생존을 가정하지 않는다. reconnect 후 미확정 publish는 동일 ID로 재발행하고 consumer inbox가 중복을 흡수한다. ACK의 channel/delivery tag를 다른 channel에 재사용하지 않는다. [RabbitMQ routing·영속화와 confirm](https://www.rabbitmq.com/docs/confirms)

### Kafka와 RabbitMQ를 함께 쓰는 예

시장 수집 서비스가 observation과 outbox를 저장하고 Kafka에 발행한다. signal/research 서비스는 Kafka를 읽어 자신의 inbox와 durable job intent를 같은 DB에 저장한다. 그 서비스의 outbox relay가 RabbitMQ로 `RunExperiment(command_id)`를 전달한다. Python worker는 연구 job store의 claim/lease를 획득하고 immutable artifact를 만든 뒤 job outcome/outbox를 저장한다. 완료 사실은 Kafka로 발행되어 portfolio/research read model을 갱신한다.

bridge는 Kafka 소비 offset을 진행하기 전에 자신의 inbox+job intent를 commit한다. Rabbit publish 후 bridge가 죽어도 같은 command ID가 재전달되며 job authority가 중복 실행 결과를 막는다. Kafka와 RabbitMQ에 동시에 publish하는 함수로 이 절차를 대체하지 않는다. 같은 업무 명령을 두 broker의 소비자가 각각 실행하게 하지 않는다. routing 변경 때도 command ID와 기존 owner를 유지한다.

### 순서·재처리·DLQ·보관

retry는 transient 오류만 backoff+jitter와 총 attempt/age 상한 내에서 수행한다. 영구 validation/권한 오류는 결과를 기록하고 격리한다. 작업 만료는 소비 시 다시 확인한다. dead-letter는 별도 durable 기록으로 옮긴 것이 확인된 뒤 원본을 완료 처리한다. RabbitMQ의 기본 dead-letter 설정이 손실 없는 전달을 보장한다고 가정하지 않고 queue type/version에 맞는 보장과 장애 테스트를 확인한다. [RabbitMQ quorum queues](https://www.rabbitmq.com/docs/quorum-queues)

DLQ replay는 운영자가 원인과 schema·권한·만료를 재검증하는 별도 명령이다. 단순 재전송은 같은 ID를 유지하고 payload가 바뀌면 새 command ID와 supersedes 관계를 기록한다. broker retention, outbox 보관, inbox dedup 보관은 지원하는 최대 replay 기간과 맞춘다. dedup 기록이 이미 사라진 과거 event를 운영 consumer에 무작정 다시 넣지 않는다.

projection rebuild는 새 namespace와 checkpoint에서 실행한다. side-effect consumer(주문·알림)와 분리하여 과거 사실 재생이 외부 행동을 다시 발생시키지 않게 한다. snapshot+watermark 이후 event를 적용하고 catch-up 검증 후 조회를 전환한다. outbox 삭제는 모든 필수 destination의 확인과 복구 보관 기준을 충족한 뒤 수행한다.

메시지 크기·prefetch/batch·in-flight·consumer concurrency·lag·disk watermark를 제한한다. broker 중단 시 outbox는 예산 안에서만 쌓는다. 한도 도달 시 중요 durable write와 event를 함께 받을 수 없는 명령을 명시 거절한다. 이미 진행된 주문의 결과 기록·kill switch·reconciliation 경로용 용량과 우선순위는 별도로 보장한다.

## 7. 인프라 구성과 교체 계약

bootstrap의 typed profile이 DB/cache/event-publisher/replay-source/job-queue/auth adapter를 선택한다. domain/application에 driver name switch를 넣지 않는다. 설정은 구성의 source of truth 한 곳에서 검증하고 secret은 reference만 둔다. 지원하지 않는 조합은 시작 시 거부한다. 예: SQLite 다중 writer replica, remote without auth/TLS, durable event를 메모리 publish만으로 처리, transaction-required use case를 transaction 없는 store에 연결.

adapter 교체는 schema와 의미 검증을 포함한다. DB·MQ·cache별 contract suite는 실제 backend를 Podman에서 올려 실행한다. fake는 빠른 domain 테스트에만 쓰고 commit loss, reconnect, redelivery, pool, isolation을 증명하는 대신 사용하지 않는다. CI matrix는 지원한다고 선언한 프로필만 포함한다. 미구현 adapter는 문서의 후보일 뿐 지원 목록에 넣지 않는다.

## 8. 서비스 분리·운영·복구 gate

서비스를 분리하기 전에 다음을 모두 만족한다.

- 데이터 writer와 contract owner가 하나이며 cross-service table access가 없다.
- workload 측정에서 독립 scaling/배포/장애 격리의 이익을 확인했다.
- API/event schema 호환성, N/N-1 rolling upgrade, timeout·retry·idempotency·취소가 정의됐다. strict 기존 decoder 때문에 additive field도 무조건 안전하다고 가정하지 않는다.
- resource 권한과 service identity, secret 분리, network egress/ingress가 검증됐다.
- outbox/inbox 또는 동등한 durable handoff, reconciliation, projection rebuild가 검증됐다.
- 지표·알림·runbook·backup/restore와 운영 owner가 있다.

추출 순서는 연구/수집 → 읽기 projection/알림 → 필요 시 identity 업무 기능 → trading authority 순으로 평가한다. 마지막 authority도 내부 모듈은 유지한다. 서비스 수를 늘리는 것을 목표로 삼지 않는다.

전환은 기존 owner에서 event/contract를 먼저 제공하고, 새 service를 side-effect 없는 shadow로 검증한 다음 routing을 한 번에 한 owner로 넘긴다. writer drain과 lease fencing 이후에 새 owner를 시작한다. old/new binary가 같은 계좌를 독립 실행하지 않는다. rollback도 writer 정지와 미해결 dispatch 확인을 거치며 신규 데이터가 생긴 DB를 버리지 않는다.

관측 항목은 request p95/p99, DB lock/pool wait, outbox oldest age, consumer lag/gap, job age/attempt, cache age, provider quota, unknown dispatch, lease expiry, rejected authority, projection revision 차이다. trace/correlation ID는 HTTP→outbox→consumer→결과까지 유지하되 broker secret·원계좌·payload 전문을 기록하지 않는다. 금융/보안 audit은 sampling하지 않는다.

목표 수치는 측정 없이 확정하지 않는다. 각 프로필 출시 전에 동시 계좌/strategy/시세량/연구 job 입력으로 load scenario를 고정하고 latency·freshness budget, recovery time, 허용 데이터 손실을 서명된 운영 기준으로 정한다. 거래 기록은 확인된 commit을 임의 유실해도 되는 데이터로 취급하지 않는다. 백업만 있다고 HA/PITR 또는 RPO=0을 주장하지 않으며 복구 drill과 quorum/failover 동작을 별도로 검증한다.

스케줄러는 job 생성만 소유하고 실행 authority는 DB claim/lease/fence가 소유한다. autoscaling이나 CronJob의 중복을 정상 장애 조건으로 시험한다. 종료 시 신규 claim 차단 → 진행 작업 drain/기록 → lease 해제·연결 종료 순서를 따른다. kill switch와 emergency reconciliation은 분석·cache·MQ 상태와 독립적으로 사용할 수 있어야 한다.

## 9. 확장 시나리오별 검증

| 시나리오 | 통과 조건 |
| --- | --- |
| 사용자 A가 B의 account alias/API/event/cache key를 제출 | 모든 경계에서 거절, 존재 여부/잔고/결과 미노출 |
| trading revoke commit 직후 대기 중인 자동 command 실행 | 같은 authority lock/epoch에서 이전 권한 거절. 먼저 dispatch commit된 주문은 in-flight로 보고 reconciliation |
| PostgreSQL 동시 예약·serialization failure | 한도 초과·중복 예약 없음, DB 단계만 bounded retry |
| DB commit 응답 유실 | receipt로 결과 확인, unknown command 재실행 금지 |
| Redis 장애·stale refill·invalidation 유실 | 권한/주문 판단 영향 없음, 제한된 DB fallback, freshness 표시 |
| outbox commit 후 relay crash / publish 후 표시 전 crash | event 누락 없음, 같은 ID 중복을 consumer가 흡수 |
| consumer DB commit 후 ACK 전 crash | effect 1회, 재전달은 inbox 처리 |
| Kafka rebalance/partition 순서 gap·뒤 record 선완료 | 완료된 수신 prefix 이전의 미완료 record를 건너뛰는 offset commit 없음, projection revision gap 복구 |
| RabbitMQ reconnect/confirm 유실/redelivery·unroutable·재시작 | 필수 destination routing/영속화 확인 전 delivered 금지, durable command ID 유지와 중복 job effect 없음 |
| Kafka→Rabbit bridge 중간 장애 | durable job intent에서 재개, 두 broker 간 exactly-once 가정 없음 |
| replay/보관 만료/미지원 schema | live side effect 재실행 없음, 격리·재구축 기준 준수 |
| 새 DB 쓰기 후 이전 DB로 rollback 시도 | 즉시 전환 차단, 검증된 역이전/forward fix 경로 사용 |
| broker/MQ/cache/IdP 일부 장애 | 명시한 읽기 저하와 신규 실행 차단, kill switch·기존 결과 기록 보존 |

## 10. 현재 refactor와 미래 확장 구현의 구분

이번 refactor에서 만드는 것은 owner 분리, consumer port, 의존성 검사, composition과 기존 동작 보존이다. tenant migration, PostgreSQL, OIDC/BFF, Redis, outbox/inbox, Kafka/RabbitMQ adapter와 새 서비스는 이 설계에 따라 **별도 구현 단계**로 진행한다. 지원하지 않는 profile을 설정 항목만 먼저 추가하지 않는다.

다음 상세 구현 설계 시 확정할 입력은 실제 동시 사용자·계좌·event 양, 배포 위치, 운영 가능한 서비스 수, IdP/secret manager, 복구·보관 목표다. 그 값이 미정이어도 업무 소유권·원자성·중복 방지·격리 규칙은 확정할 수 있다. 제품/버전 선정과 용량 수치는 해당 단계에서 측정·공식 문서 확인 후 정한다.
