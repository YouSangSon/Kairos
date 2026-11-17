# Ubuntu에서 개인 개발 core 실행하기

Ubuntu 24.04 amd64에서 rootless Podman 4.9.3과 사용자 Quadlet으로 Go core 하나를 실행한다. 호스트의 `127.0.0.1:18080`만 열고 개인 PC는 SSH tunnel로 접근한다. 개발 DB는 `kairos-dev-data` volume에 둔다. 기존 PC DB, Docker와 MicroK8s 자원은 사용하지 않는다.

이 절차는 [G6의 단일 노드 개발 허용 범위](../../gates/g6-deployment.md)와 [운영 설계 §9](../architecture/production-and-improvement.md#9-운영보안관측복원)를 따른다. private remote P2a나 G6 production 승격 근거가 아니다. OIDC, public TLS, cloud 운영, live 주문은 구현·검증하지 않는다. API는 서버의 로컬 사용자를 신뢰하므로 공유 계정이나 불신 사용자가 있는 호스트에 적용하지 않는다. paper runner, 전략 자동 실행과 demo seed도 시작하지 않는다.

## 1. 실행 사용자와 선행 조건 확인하기

이 문서의 shell 명령은 Ubuntu의 같은 일반 사용자로 `bash`에서 실행한다. `sudo podman`을 섞지 않는다. 관리자 설치와 linger 변경은 호스트 소유자의 승인을 받은 뒤 한 번 수행한다.

```sh
set -euo pipefail
sudo apt-get update
sudo apt-get install --yes podman uidmap slirp4netns fuse-overlayfs dbus-user-session git curl python3
sudo loginctl enable-linger "$(id -un)"
podman --version
id
cat /etc/subuid /etc/subgid
podman info --format '{{.Host.Security.Rootless}} {{.Host.CgroupsVersion}}'
loginctl show-user "$(id -un)" -p Linger
systemctl --user is-system-running
ss -ltn '( sport = :18080 )'
dpkg -L podman | sed -n '/system-generators/p;/quadlet$/p'
```

Podman 4.9.3, rootless `true`, cgroup `v2`, 유효한 subuid/subgid 범위와 `Linger=yes`를 확인한다. 대상 호스트의 사용자 UID는 1000이고 subordinate 범위는 `100000:65536`이다. 18080에 기존 listener가 있으면 먼저 소유자를 확인한다. 기존 컨테이너·사용자 unit·다른 서비스는 중지하거나 삭제하지 않는다. user systemd가 degraded이면 실패 unit을 확인한 뒤 진행한다.

아래 `KAIROS_GENERATOR`에는 위에서 확인한 실제 executable 경로를 넣는다. 배포판 경로를 추측해 대체하지 않는다.

```sh
KAIROS_GENERATOR=/usr/libexec/podman/quadlet
test -x "$KAIROS_GENERATOR"
test "${XDG_CONFIG_HOME:-$HOME/.config}" = "$HOME/.config"
KAIROS_STATE="$HOME/.local/state/kairos-dev"
KAIROS_UNIT_DIR="$HOME/.config/containers/systemd"
export KAIROS_STATE KAIROS_UNIT_DIR
python3 - <<'PY'
import os
from pathlib import Path
home = Path.home()
for relative in ('.local', '.local/state', '.local/state/kairos-dev',
                 '.config', '.config/containers', '.config/containers/systemd'):
    path = home / relative
    if path.is_symlink():
        raise SystemExit(f'refuse symlink: {path}')
    if not path.exists():
        path.mkdir(mode=0o700)
    st = path.stat()
    if not path.is_dir() or st.st_uid != os.getuid() or st.st_mode & 0o022:
        raise SystemExit(f'review ownership/permissions: {path}')
PY
test "$(systemctl --user show kairos-dev.service -p LoadState --value)" = not-found
podman ps -a --filter name='^kairos-dev-core$'
python3 - <<'PY'
import os
from pathlib import Path
for path in (Path(os.environ['KAIROS_UNIT_DIR']) / 'kairos-dev.container',
             Path.home() / '.config/systemd/user/kairos-dev.service'):
    if path.exists() or path.is_symlink():
        raise SystemExit(f'existing unit requires ownership review: {path}')
PY
```

최초 설치에서는 같은 이름의 unit·container·template이 없어야 한다. 존재하면 경로, 소유 UID, 내용과 `io.kairos.runtime=ubuntu-development` label을 대조한다. 다른 작업이거나 소유권이 불명확하면 중단한다. Quadlet generator는 같은 이름의 기존 service를 가릴 수 있다. 조회 실패를 부재로 해석하지 않는다.

## 2. source SHA와 immutable image 기록하기

검토한 checkout의 저장소 루트에서 실행한다. 기존 [core.Dockerfile](../../infra/core.Dockerfile)을 사용하며 Go 버전은 `.tool-versions`와 같은 1.27.4다. 빌드에는 base image와 Go module 다운로드가 필요할 수 있다. runtime에 credential은 전달하지 않는다.

```sh
git diff --quiet
git diff --cached --quiet
KAIROS_SOURCE_SHA="$(git rev-parse HEAD)"
test ! -e "$KAIROS_STATE/image-$KAIROS_SOURCE_SHA.id"
KAIROS_BUILD="$(mktemp -d "$KAIROS_STATE/source.XXXXXX")"
git archive "$KAIROS_SOURCE_SHA" | tar -x -C "$KAIROS_BUILD"
podman build --file "$KAIROS_BUILD/infra/core.Dockerfile" \
  --label "org.opencontainers.image.revision=$KAIROS_SOURCE_SHA" \
  --iidfile "$KAIROS_STATE/image-$KAIROS_SOURCE_SHA.id" "$KAIROS_BUILD"
KAIROS_IMAGE_ID="$(cat "$KAIROS_STATE/image-$KAIROS_SOURCE_SHA.id")"
podman image inspect --format '{{.Id}} {{.Os}}/{{.Architecture}} {{index .Labels "org.opencontainers.image.revision"}}' "$KAIROS_IMAGE_ID"
export KAIROS_SOURCE_SHA KAIROS_IMAGE_ID
KAIROS_RENDER="$(mktemp -d "$KAIROS_STATE/render.XXXXXX")"
export KAIROS_RENDER
python3 - <<'PY'
import os, re
from pathlib import Path
image, sha = os.environ['KAIROS_IMAGE_ID'], os.environ['KAIROS_SOURCE_SHA']
assert re.fullmatch(r'sha256:[0-9a-f]{64}', image), 'require full local image ID'
assert re.fullmatch(r'[0-9a-f]{40}', sha), 'require full source SHA'
source = Path('infra/ubuntu-dev/kairos-dev.container.in').read_text()
rendered = source.replace('@IMAGE_ID@', image).replace('@SOURCE_SHA@', sha)
assert '@IMAGE_ID@' not in rendered and '@SOURCE_SHA@' not in rendered
target = Path(os.environ['KAIROS_RENDER']) / 'kairos-dev.container'
with target.open('x') as output:
    output.write(rendered)
target.chmod(0o600)
PY
QUADLET_UNIT_DIRS="$KAIROS_RENDER" "$KAIROS_GENERATOR" --user --dryrun \
  > "$KAIROS_RENDER/generator.dryrun" 2>&1
cat "$KAIROS_RENDER/generator.dryrun"
```

빌드 context는 기록한 SHA의 `git archive`만 포함한다. image inspect의 ID와 source SHA가 기록과 같고 platform이 `linux/amd64`인지 확인한다. generator 출력에 오류가 없어야 한다. `ExecStart`의 image ID, `--pull never`, UID/GID 10001, keep-id mapping, named volume, loopback publish, 보안·자원 제한을 확인한다. service `active`는 API 준비 완료를 뜻하지 않는다.

template은 Podman 4.9.3의 [Quadlet key와 generator](https://github.com/containers/podman/blob/v4.9.3/pkg/systemd/quadlet/quadlet.go), [user namespace](https://github.com/containers/podman/blob/v4.9.3/docs/source/markdown/options/userns.container.md), [log 제한](https://github.com/containers/podman/blob/v4.9.3/docs/source/markdown/options/log-opt.md)을 기준으로 한다. 해당 버전에 전용 Quadlet key가 없는 CPU·memory·stop timeout·log option은 `PodmanArgs`로 전달한다. generator의 기본 `--replace`는 마지막 `--replace=false`로 덮어쓴다. 같은 이름의 container가 남아 있으면 시작이 실패하며 기존 container를 교체하지 않는다. 소유권과 CID를 확인한 뒤 충돌을 해결한다. 다른 버전으로 바꾸면 dryrun과 실제 inspect를 다시 확인한다.

## 3. writer를 중지하고 명시적으로 migrate하기

최초 설치에서는 기존 writer가 없어야 한다. 이미 이 profile을 운영 중이면 소유권을 확인한 service를 먼저 `systemctl --user stop kairos-dev.service`로 중지하고 container가 종료됐는지 확인한다. 같은 volume을 사용하는 별도 CLI·수동 container도 없어야 한다. 자동 restart 중인 writer에 migrate하지 않는다.

```sh
podman ps -a --filter volume=kairos-dev-data
podman volume ls --filter name='^kairos-dev-data$'
```

최초 설치에서 volume이 없음을 확인한 경우만 생성한다. 기존 volume이면 아래 create를 생략하고 label·사용처·이전 image 기록을 확인한다. 개발 데이터 외의 volume을 재사용하지 않는다.

```sh
podman volume create --label io.kairos.runtime=ubuntu-development kairos-dev-data
podman volume inspect kairos-dev-data
podman run --rm --pull=never --network=none \
  --label io.kairos.runtime=ubuntu-development \
  --user=10001:10001 --userns=keep-id:uid=10001,gid=10001 \
  --read-only --read-only-tmpfs=false --cap-drop=all \
  --security-opt=no-new-privileges \
  --tmpfs=/tmp:rw,nosuid,nodev,noexec,size=64m,mode=1777 --shm-size=16m \
  --cpus=0.75 --memory=1536m --memory-swap=1536m --pids-limit=256 \
  --log-driver=k8s-file --log-opt=max-size=10mb \
  --volume=kairos-dev-data:/data "$KAIROS_IMAGE_ID" \
  migrate -db /data/omni-folio.db
```

이 migrate는 service와 같은 image ID·volume·user namespace를 사용한다. 새 volume의 초기 복사와 keep-id mapping으로 UID 10001이 쓴다. 호스트 DB 경로의 `chown`이나 `:U`를 사용하지 않는다. 실패하면 DB와 volume을 보존하고 원인을 확인한다. 성공 전에는 service를 시작하지 않는다. `migrate`는 모든 start나 restart에 자동 실행하지 않는다. 기존 원장의 replay 전환이 필요하면 [별도 전환 절차](../../gates/g1p-chronological-ledger-replay.md#로컬-전환-절차)를 따른다.

## 4. 설치하고 상태·종료·재시작 확인하기

최초 설치에서는 목적지가 없다는 위 확인을 재검증한다. 아래 exclusive create는 기존 파일이나 symlink를 덮어쓰지 않는다.

```sh
python3 - <<'PY'
import os
from pathlib import Path
source = Path(os.environ['KAIROS_RENDER']) / 'kairos-dev.container'
target = Path(os.environ['KAIROS_UNIT_DIR']) / 'kairos-dev.container'
fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'wb') as output:
    output.write(source.read_bytes())
PY
systemctl --user daemon-reload
systemctl --user cat kairos-dev.service
systemctl --user start kairos-dev.service
systemctl --user status kairos-dev.service --no-pager
curl --fail --silent --show-error http://127.0.0.1:18080/readyz
podman inspect kairos-dev-core
podman logs --tail=50 kairos-dev-core
ss -ltn '( sport = :18080 )'
systemctl --user restart kairos-dev.service
curl --fail --silent --show-error --retry=10 --retry-connrefused --retry-delay=1 \
  http://127.0.0.1:18080/readyz
systemctl --user stop kairos-dev.service
test "$(systemctl --user is-active kairos-dev.service || test "$?" = 3)" = inactive
ss -ltn '( sport = :18080 )'
systemctl --user show kairos-dev.service -p Result -p ExecMainCode -p ExecMainStatus
```

inspect에서 image ID, user mapping, port, read-only, capability, no-new-privileges, tmpfs, log 크기와 CPU 0.75·memory 1536 MiB·pids 256을 확인한다. stop 뒤 inactive와 port 해제를 확인한다. `is-active`의 inactive exit 3은 이 단계의 예상 결과다. 다시 개발하려면 `systemctl --user start kairos-dev.service`를 실행하고 readiness를 확인한다. 생성된 transient service에 `enable`하지 않는다. template의 `WantedBy=default.target`과 linger가 사용자 service의 재부팅 기동을 맡는다.

SIGINT/SIGTERM을 받으면 새 handler 진입을 막고 HTTP 요청에 10초 유예를 준다. 유예 중에는 진행 중인 요청의 DB를 유지한다. 초과하면 연결을 닫고 요청 context를 취소하며 오류로 종료한다. handler가 취소를 무시하면 DB를 먼저 닫지 않고 기다린다. 이 경우 20초 container stop 또는 30초 systemd 제한에서 강제 종료될 수 있으므로 drain·cleanup 완료로 기록하지 않는다. 다음 시작의 readiness와 데이터 상태를 다시 확인한다. 일반 stop은 exit 0이며 `Restart=on-failure`는 이를 재기동하지 않는다.

PC에서는 기존 SSH alias를 사용한다.

```sh
ssh -N -L 127.0.0.1:18080:127.0.0.1:18080 my-ubuntu-server
curl --fail --silent --show-error http://127.0.0.1:18080/readyz
```

PC의 18080이 사용 중이면 tunnel의 왼쪽 포트만 바꾼다. web client를 연결하려면 exact browser origin을 검토해 `serve -allow-origin`에 명시하고 다시 렌더링한다. 현재 template은 CORS origin을 추가하지 않는다.

## 5. backup과 독립 복원 검증하기

개발 쓰기를 멈추고 snapshot 취득부터 stop까지 다른 클라이언트·CLI가 쓰지 못하게 한다. running API에서 현재 snapshot을 받은 뒤 service를 중지한다. 쓰기가 끼어들면 golden 검증이 실패하므로 원인을 해결하고 새 디렉터리에서 다시 수행한다.

```sh
KAIROS_BACKUP="$(mktemp -d "$KAIROS_STATE/backup.XXXXXX")"
chmod 700 "$KAIROS_BACKUP"
curl --fail --silent --show-error http://127.0.0.1:18080/v1/portfolio/snapshot \
  > "$KAIROS_BACKUP/snapshot.json"
chmod 600 "$KAIROS_BACKUP/snapshot.json"
systemctl --user stop kairos-dev.service
podman ps --filter volume=kairos-dev-data
podman run --rm --pull=never --network=none \
  --user=10001:10001 --userns=keep-id:uid=10001,gid=10001 \
  --read-only --read-only-tmpfs=false --cap-drop=all \
  --security-opt=no-new-privileges \
  --tmpfs=/tmp:rw,nosuid,nodev,noexec,size=64m,mode=1777 --shm-size=16m \
  --cpus=0.75 --memory=1536m --memory-swap=1536m --pids-limit=256 \
  --log-driver=k8s-file --log-opt=max-size=10mb \
  --volume=kairos-dev-data:/data --volume="$KAIROS_BACKUP:/backup" \
  "$KAIROS_IMAGE_ID" backup -db /data/omni-folio.db \
  -out /backup/candidate.db -golden /backup/snapshot.json -manifest /backup/manifest.json
cp --no-clobber "$KAIROS_BACKUP/candidate.db" "$KAIROS_BACKUP/restored.db"
podman run --rm --pull=never --network=none \
  --user=10001:10001 --userns=keep-id:uid=10001,gid=10001 \
  --read-only --read-only-tmpfs=false --cap-drop=all \
  --security-opt=no-new-privileges \
  --tmpfs=/tmp:rw,nosuid,nodev,noexec,size=64m,mode=1777 --shm-size=16m \
  --cpus=0.75 --memory=1536m --memory-swap=1536m --pids-limit=256 \
  --log-driver=k8s-file --log-opt=max-size=10mb \
  --volume="$KAIROS_BACKUP:/backup" "$KAIROS_IMAGE_ID" \
  verify-restore -db /backup/restored.db \
  -golden /backup/snapshot.json -manifest /backup/manifest.json
sha256sum "$KAIROS_BACKUP/candidate.db" "$KAIROS_BACKUP/restored.db"
systemctl --user start kairos-dev.service
```

`backup`은 일관된 candidate와 manifest를 생성하고 golden snapshot을 비교한다. `verify-restore`는 독립 복사본의 hash·schema·owner proof를 검증한다. verifier에는 원본 data volume을 연결하지 않는다. 이 검증은 실제 원본 교체나 production 복구 리허설 완료를 뜻하지 않는다. 실패하면 원본·backup·오류 근거를 보존한다. 성공한 backup의 DB·snapshot·manifest·source SHA·image ID를 함께 접근 제한된 별도 위치에도 보관한다. live DB 파일만 복사하거나 volume을 삭제해 재시작하지 않는다.

## 6. 변경·rollback과 정리하기

업데이트 전 현재 source SHA·image ID·template·backup 검증 결과를 보존한다. 소유권이 확인된 기존 service를 중지하고 모든 writer 종료를 확인한 뒤, 새 image로 필요한 migrate를 명시적으로 수행한다. 새 template은 별도 render 디렉터리에서 dryrun한다. 기존 template은 현재 사용자 소유의 일반 파일이며 기록한 내용과 같음을 확인한 후 사본을 보존하고 교체한다. service drop-in, symlink나 다른 소유자의 unit은 자동 교체하지 않는다.

schema를 바꾸지 않았고 양쪽 binary 호환성을 확인한 경우만 보존한 image ID·template으로 되돌린다. 새 schema에 쓴 뒤 옛 binary를 바로 실행하지 않는다. 검증한 forward fix를 우선하고, backup 복원은 별도 사본과 손실 범위를 검토한 뒤 수행한다. 실패한 migrate 뒤 DB를 삭제하거나 이전 backup으로 조용히 덮어쓰지 않는다.

검증이 끝나면 이번 작업이 만든 stopped 임시 container와 더 이상 필요 없는 source·render 임시 파일만 이름·소유권을 대조해 정리한다. 실행에 필요한 image·설치 template·data volume, backup과 검증 근거는 보존한다. `podman system prune`, volume 일괄 삭제, Docker/MicroK8s 정리는 하지 않는다. 잔여 목록과 삭제·보존 이유를 작업 기록에 남긴다.
