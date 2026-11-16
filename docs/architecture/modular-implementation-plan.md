# 모듈형 아키텍처 구현 계획

- 기준: `25f077a`, 2026-11-09 사용자 구현 요청.
- 설계 정본: [전체 리팩토링 설계](whole-project-refactoring.md) 15절과 15.12절.
- 상태: PR #80의 모듈 경계 이전 뒤, 2026-11-16 재검토에서 확인한 업무 규칙 소유권의 잔여 항목도 구현하고 특성화·집중 검사와 독립 소스 검토를 마쳤다. 보완 단위의 최종 실행 결과는 아래 검증 범위와 해당 PR의 검증 기록으로 구분한다. 배포 완료를 뜻하지 않는다.
- 완료 조건: owner별 private 구현과 작은 공개 계약, 모든 호출자 이전과 기존 경로 제거, 순환/계층/SQL 소유권 검사의 실패 fixture와 통과 근거, 기존 wire/금액/hash/원자성 보존, 독립 리뷰·저장소 검사·작업 소유 자원 정리.

## 단계와 소유 범위

| 단위 | 변경 범위와 작업 | 검증/종료 기준 | 상태 |
| --- | --- | --- | --- |
| 공유 근거 | 기존 설계/체크포인트 복원, 계약·migration hash 고정, 정확한 변경 허용 목록 | 기존 사용자 변경 보존, agent별 worktree/소유 경로 확정 | 소유별 작업 트리·계약 hash 복원 완료 |
| Go 자동 경계 | `services/core/internal/architecture/`에 module graph/owner registry/SQL·공개 계약·catalog 예외 검사 | 기존 검사 보존, 새 negative fixture RED→GREEN. 부분 이식의 legacy 예외는 목록 고정 후 감소만 허용 | 구현·negative fixture·독립 리뷰·최종 전체 검사 통과. legacy 예외는 변경하지 않은 exact 한 파일만 유지 |
| Go 저장소 관측 | `services/core/internal/storage/{contracts,observation}/`와 composition coordinator | MOD-11–16 실제 SQLite/수명/실패/경합 검사. 원본 Tx/쓰기 우회 차단 | 실제 owner 연결 MOD-11/16, 동일 ref의 수명·권한·rollback과 최종 전체 검사 통과 |
| Go 원장 | `services/core/internal/modules/ledger/`와 기존 ledger 구현·호출자·테스트 | 원장/종목 자격/분할/broker 대조 공개 계약. private 내부·자기 table만 사용. 동일 tx·receipt·FIFO 보존 | private domain/application/SQLite·HTTP·CLI와 root 호출자 이전 완료. 특성화·집중·최종 전체 검사 통과 |
| Go 시장·평가·관심 종목 | 각 `internal/modules/` owner와 기존 root 대응 함수 | 원장 공개 자격/관측 참조 사용, atomic owner-price import, 평가/quote 같은 snapshot 유지 | private 구현·공개 관측 계약·HTTP/CLI·root 이전 완료. 신규 가격 기록 시계 1회/재시도·충돌 0회 회귀 검사와 최종 전체 검사 통과 |
| Go 거래·유지보수 | 거래 전체 transaction 호출 묶음과 owner proof·migration·backup/restore | admission/fill/policy/promotion/runner/dispatch 원자성·순서·복구 보존, source/candidate proof 동등성 | private 업무 조율·기존 테스트 이전 완료. 실제 SQLite·프로세스·비어 있지 않은 과거 schema 복구·취소 검사 및 독립 리뷰·최종 전체 검사 통과 |
| Flutter | `apps/client/lib`, `test` 및 영향받는 동일 앱 계약 검사 | WIP 두 실패 해결, 7 feature 실제 repository/codec 소유, session 보존, pure domain/application, 순환 검사 | 로컬 통합91d5462, analyzer/498개 검사/web release 통과. 독립 리뷰 3건 보완 후 집중 30개 검사·분석 통과, 보완본 재검토 잔여 blocker 없음. PR #78 Android CI의 전체 검사·web·ARM64 release 통과 |
| Python | `services/research/tests/test_boundary.py` 및 필요 owner 파일 | 기존 domain/application/adapter 이전 유지, 상대/절대 import cycle과 계층 위반을 자동 거부 | 독립 검사 전체 67개·PR #78 Core CI 통과. 후속 Go 통합 검사는 별도 |
| 통합 | root `.gitignore`, 필요 Makefile·CI·문서 및 통합 diff | 최종 `make check` 1회, `make smoke`, 필요한 Podman/클라이언트 release 경로, 독립 리뷰, local commit과 cleanup | 최종 수정본의 `make check`·`make smoke`·Podman 빌드와 실행 6개 검사 통과. root production 77개·기존 internal 24개 제거, 기존 root 검사 568개의 이전 331개/유지 237개 대조 완료. 원격 CI·PR 검증은 별도 |

Go의 실제 파일별 수정 허용 목록과 시그니처는 각 owner 작업의 첫 기록에서 고정한다. 원자적 closure를 자를 수 없다면 같은 단위로 옮기며 root Service를 통째로 internal/core에 옮기지 않는다. 생성만 한 폴더/빈 인터페이스/전달 wrapper를 완료로 세지 않는다.

PR #80의 2026-11-16 로컬 검사에서는 Go testcase event 2,680개 통과·2개 skip(선택 실행인 history profile·real-clock soak)·실패 0개, Python 연구 검사 67개 통과, JSON 계약 48개 파싱을 확인했다. Go/Python 검사는 asdf의 Go 1.24.5·Python 3.14.5를 지정한 호스트 실행이며, Podman에서는 최종 소스 이미지 빌드와 실제 HTTP·멱등 반영·금액·비공개 ID 제외를 확인했다. Flutter 소스는 PR #78 이후 변경하지 않았으므로 위 표의 analyzer·498개 검사·web·Android release 근거를 재사용했다. 공개 계약 53개·기존 SQL migration 27개·client 179개·Python 46개 파일의 기준 main 대비 바이트 동일성도 대조했다. 작업 전용 캐시·임시 경로·컨테이너·볼륨·이미지만 정리하며 기존 리소스는 보존한다. iOS release·실제 키움 인증·운영 배포는 검증하지 않았다.

## 업무 규칙 소유권 보완 — 2026-11-16

기준은 PR #80의 main `e62ff88`이다. 추가 사용자 지시에 따라 Go는 2026-11-16의 [공식 배포 목록](https://go.dev/dl/)에서 확인한 최신 안정판 1.27.4로 올리고 루트 `.tool-versions`를 언어 버전 정본으로 유지한다. core의 버전 파일은 루트를 가리키며 CI·컨테이너도 같은 Go를 사용한다. 이전 Go 1.24.5 검사는 특성화 근거로만 남기고 최종 통합은 새 버전에서 수행한다. import·SQL 경계와 회귀 검사 통과만으로 정책 판단의 소유 계층까지 완료됐다고 판단하지 않는다. 각 모듈의 실제 함수·호출자를 대조하여 업무 허용 조건과 계산·상태 전이는 domain으로 이전하고, application의 조회·시간/ID 획득·명명된 transaction 조율과 adapter의 해석·SQL·schema·저장 증명은 유지한다.

| 단위 | 보완 범위 | 상태 |
| --- | --- | --- |
| 원장 | 현금 정정 자격·정확한 반전·중복 사용, source 동등성·미해결 입력, replay·broker 자격, preview 만료·적용 가능 여부·revision | domain 직접 검사 RED→GREEN, 기존 오류·조회 순서 특성화와 집중 통합 검사 통과. 독립 소스 검토의 잔여 지적 해소 |
| 거래 | 주문·권한·예약·lease·승격·성과·수량 투영·전략 전이·입력 자격, monitor 상태와 평가 대상 판정 | A/B/C 정책별 호출자 대조·domain 이전·집중 검사·독립 소스 검토 완료. SQL 호출 209개와 canonical record 필드·순서·tag 보존 |
| 시장·유지보수 | 가격/버전/출처/시간/상장/typed 입력, backup profile·활성화·legacy 자격 | domain 추출·모듈 집중 검사·독립 소스 검토 완료. 세 import의 신규/재시도/충돌에 대한 기록 시계·callback 순서 보존 |
| Python | 후보/목표 수량과 입력 전략·수량·fold·비용·OHLC·시간 자격 | domain 추출·집중 63개 검사·독립 소스 검토 완료. 기준/현재 호환 검사 6개 및 CLI 산출물 6종의 byte·hash 동일성 확인 |
| Flutter·평가·관심 종목·공개 연구 | 실제 정책과 요청 수명·wire 검증·projection 구분 | portfolio의 미래 cutoff와 watchlist revision·변경·metadata 불변조건을 domain으로 이전하고 집중 검사·독립 소스 검토 완료. Flutter·공개 연구는 실제 호출 경로 대조 결과 추가 이전 불필요 |

기존 분기의 위치를 유지한다. 먼저 읽던 값과 오류를 나중으로 옮기거나 모든 facts를 먼저 모으지 않는다. JSON 필드 해석·hash·schema·저장 증거의 동등성은 domain에 복제하지 않는다. legacy/v1/v2 허용 범위와 부재/null, 반환 오류·clock·ID 횟수도 보존한다. 최종 통합 뒤 전체 검사·Podman·독립 리뷰·동일 head CI를 확인하며 이전 PR의 통과를 변경된 소스의 통과로 대신하지 않는다.

업무 규칙과 외부 처리의 분류 근거는 [설계 15.15절](whole-project-refactoring.md#1515-업무-규칙의-의미적-소유권-보완)에 남긴다. 함수 목록은 실제 caller와 정책을 찾기 위한 조사 범위이며, 선언 개수나 자동 import 검사만으로 모든 업무 의미를 증명했다고 해석하지 않는다.

최종 검증은 Go 1.27.4·Python 3.14.5의 `make check`, `make smoke`, 최종 소스의 Podman 빌드·실행, 기준/현재 실행 파일의 독립 HTTP·CLI·SQLite 비교와 같은 head의 Core·Android CI를 사용한다. Flutter 코드는 바꾸지 않았으며 로컬 근거는 기존 검사를 재사용하고 `.tool-versions` 변경으로 실행되는 Android CI에서 새 결과를 확인한다. Podman 실행 6개 검사와 build 입력 366개의 hash 동일성을 확인했으며 테스트용 컨테이너·볼륨·이미지는 정리했다. 초기 전체 검사는 cross-package domain literal 10건의 `go vet` 지적에서 멈췄고, 필드 이름만 명시한 수정과 대상 vet 통과 뒤 재개했다. testcase 수·선택 검사 skip·CI·독립 실행 검증·병합의 정확한 결과는 해당 PR 검증 기록에 남기며 소스 구현이나 앞선 단위의 통과와 합치지 않는다.

## 실행·검증 규칙

1. 기존 행위 이전은 현행 특성화 검사로 고정한다. 새 경계 규칙은 실패 fixture부터 실행한다.
2. 각 단위는 focused 검사 후 통과한 소스만 commit한다. 실패한 단위 위에 다음 이식을 쌓지 않는다.
3. 독립적인 Go/Flutter writer는 별도 worktree를 사용한다. Go 내부도 원장·자동 검사·나머지 구현의 소유 파일을 나누고 공유 root 수정은 먼저 조율한다.
4. 최종 통합 상태에서 저장소 필수 전체 검사를 한 번 수행한다. 동일 소스/환경의 통과 근거를 재사용하고 실패 시 원인부터 수정한다.
5. 작업 전용 cache/temp/process/container는 경로·ID로 소유권을 기록하고 종료·로그 보존 후 정리한다. 공유 캐시·기존 데이터와 다른 프로젝트 리소스는 보존한다.

로컬 상세 계획·로그·RED/GREEN 근거는 기존 Git 제외 경로 `docs/_internal/skill-outputs/whole-refactor-20261104/`를 계속 사용한다. 사용자가 구현 중간의 commit·main 병합·push를 추가 승인했다. 검증한 단위를 작업 브랜치로 push하고 보호 규칙에 따라 PR의 독립 검증과 필수 CI를 통과한 뒤 main에 병합한다. 직접 main push가 거절된 보호 규칙은 유지한다. 배포·실제 broker 인증/투자·credential 변경·새 인프라 설치·Rust 전환·사용자 코드 실행 호스트는 포함하지 않는다.
