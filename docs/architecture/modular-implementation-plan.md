# 모듈형 아키텍처 구현 계획

- 기준: `25f077a`, 2026-11-09 사용자 구현 요청.
- 설계 정본: [전체 리팩토링 설계](whole-project-refactoring.md) 15절과 15.12절.
- 상태: 구현 진행 중. 구현·검증·배포를 구분해 아래 단위를 갱신한다.
- 완료 조건: owner별 private 구현과 작은 공개 계약, 모든 호출자 이전과 기존 경로 제거, 순환/계층/SQL 소유권 검사의 실패 fixture와 통과 근거, 기존 wire/금액/hash/원자성 보존, 독립 리뷰·저장소 검사·작업 소유 자원 정리.

## 단계와 소유 범위

| 단위 | 변경 범위와 작업 | 검증/종료 기준 | 상태 |
| --- | --- | --- | --- |
| 공유 근거 | 기존 설계/체크포인트 복원, 계약·migration hash 고정, 정확한 변경 허용 목록 | 기존 사용자 변경 보존, agent별 worktree/소유 경로 확정 | 진행 |
| Go 자동 경계 | `services/core/internal/architecture/`에 module graph/owner registry/SQL·공개 계약·catalog 예외 검사 | 기존 검사 보존, 새 negative fixture RED→GREEN. 부분 이식의 legacy 예외는 목록 고정 후 감소만 허용 | 진행 |
| Go 저장소 관측 | `services/core/internal/storage/{contracts,observation}/`와 composition coordinator | MOD-11–16 실제 SQLite/수명/실패/경합 검사. 원본 Tx/쓰기 우회 차단 | 계획 |
| Go 원장 | `services/core/internal/modules/ledger/`와 기존 ledger 구현·호출자·테스트 | 원장/종목 자격/분할/broker 대조 공개 계약. private 내부·자기 table만 사용. 동일 tx·receipt·FIFO 보존 | 계획 |
| Go 시장·평가·관심 종목 | 각 `internal/modules/` owner와 기존 root 대응 함수 | 원장 공개 자격/관측 참조 사용, atomic owner-price import, 평가/quote 같은 snapshot 유지 | 계획 |
| Go 거래·유지보수 | 거래 전체 transaction 호출 묶음과 owner proof·migration·backup/restore | admission/fill/policy/promotion/runner/dispatch 원자성·순서·복구 보존, source/candidate proof 동등성 | 계획 |
| Flutter | `apps/client/lib`, `test` 및 영향받는 동일 앱 계약 검사 | WIP 두 실패 해결, 7 feature 실제 repository/codec 소유, session 보존, pure domain/application, 순환 검사 | 진행 |
| Python | `services/research/tests/test_boundary.py` 및 필요 owner 파일 | 기존 domain/application/adapter 이전 유지, 상대/절대 import cycle과 계층 위반을 자동 거부 | 진행 |
| 통합 | root `.gitignore`, 필요 Makefile·CI·문서 및 통합 diff | 최종 `make check` 1회, `make smoke`, 필요한 Podman/클라이언트 release 경로, 독립 리뷰, local commit과 cleanup | 계획 |

Go의 실제 파일별 수정 허용 목록과 시그니처는 각 owner 작업의 첫 기록에서 고정한다. 원자적 closure를 자를 수 없다면 같은 단위로 옮기며 root Service를 통째로 internal/core에 옮기지 않는다. 생성만 한 폴더/빈 인터페이스/전달 wrapper를 완료로 세지 않는다.

## 실행·검증 규칙

1. 기존 행위 이전은 현행 특성화 검사로 고정한다. 새 경계 규칙은 실패 fixture부터 실행한다.
2. 각 단위는 focused 검사 후 통과한 소스만 commit한다. 실패한 단위 위에 다음 이식을 쌓지 않는다.
3. 독립적인 Go/Flutter writer는 별도 worktree를 사용한다. Go 내부도 원장·자동 검사·나머지 구현의 소유 파일을 나누고 공유 root 수정은 먼저 조율한다.
4. 최종 통합 상태에서 저장소 필수 전체 검사를 한 번 수행한다. 동일 소스/환경의 통과 근거를 재사용하고 실패 시 원인부터 수정한다.
5. 작업 전용 cache/temp/process/container는 경로·ID로 소유권을 기록하고 종료·로그 보존 후 정리한다. 공유 캐시·기존 데이터와 다른 프로젝트 리소스는 보존한다.

로컬 상세 계획·로그·RED/GREEN 근거는 기존 Git 제외 경로 `docs/_internal/skill-outputs/whole-refactor-20261104/`를 계속 사용한다. 원격 push·merge·배포·실제 broker 인증/투자·credential 변경·새 인프라 설치는 이 구현 요청으로 수행하지 않는다. Rust 전환과 사용자 코드 실행 호스트도 포함하지 않는다.
