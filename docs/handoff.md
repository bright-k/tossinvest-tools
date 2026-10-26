# 다음 세션 인수인계

기준일 2026-10-26 Asia/Seoul. 작업 저장소 `/Users/bright/Desktop/work/tossinvest-tools`. [AGENTS.md](../AGENTS.md)와 실제 `git status --short --branch`를 먼저 확인한다. 기존 사용자 README·설계 문서·원본 명세의 작업 트리를 보존했다. 구현·검토 단계에서는 commit·push·배포·실제 서비스 설치를 실행하지 않았으며, 이후 사용자가 최종 변경의 스테이징·시크릿 재검사 통과 후 커밋·origin 푸시를 명시적으로 승인했다. 커밋/푸시 결과는 Git 이력과 origin/main을 기준으로 확인한다. 배포·실제 서비스 설치·키 등록·실계좌 조회는 여전히 수행 대상이 아니다. 기존 문서 작성 당시 상태는 [보관본](handoff-design-2026-10-26.md)에 남겼다.

## 현재 판정

| 구분 | 상태 |
|---|---|
| 구현 | readonly R1/R2 0.2.0 + 계좌 목록·보유 자산·현재가 CLI 확장. P0~P3 코드·실행 파일·재현 명령 제공 |
| 원본 | REST OpenAPI 3.1/API 1.2.24의 41개 작업, WS AsyncAPI 3.0/API 1.2.2의 10개 작업. 원본 JSON/manifest hash 유지 |
| REST | 전체 생성 client/type 41개. 인증+34개 조회를 실제 adapter와 mock에 연결. 6개 쓰기는 planned이며 등록·서비스·최종 전송에서 거부 |
| MCP | 독립 HTTP 서버. R1 35개/R2 39개. 공식 Go SDK로 각각 34개 REST 도구 전부와 실시간 대표 호출 확인 |
| 실제 Codex | 설치된 0.164.0-alpha.11.1 app-server에서 인증 helper와 35/39개 도구 발견 확인. 사용자 config.toml/이 채팅 등록은 변경하지 않음 |
| 사용자 대기 | 지속 MCP 등록·Restart 후 이 채팅/모델 직접 호출, Keychain 실제 등록/잠금 UX, 실계좌/IP/절전·실제 launchd 검증 |
| 주문 | 범위 제외. toss/mock의 `--mode=trading` 모두 CAPABILITY_UNAVAILABLE로 거부 |
| 실계좌 | 키 등록·토큰 발급·계좌 조회·주문 0건. live-verified 0개 |

## 실행 파일과 재현

- 실행 파일 `bin/tossinvest-tools` (macOS ARM64), `bin/SHA256SUMS`와 [빌드 기록](verification/build.txt).
- Go 1.27.3은 `.tools/go`, GOPATH/GOCACHE도 `.tools`에 한정. 시스템 Go·PATH를 설치/수정하지 않았다. 설치/개발은 scripts/bootstrap과 scripts/go를 사용한다.
- 공식 MCP SDK v1.8.0, oapi-codegen v2.8.0, runtime v1.6.0, coder/websocket v1.8.14, modernc SQLite v1.39.1, Decimal v1.4.0 고정. 정확한 전이 버전은 go.mod/go.sum.
- `make verify`: 명세/문서 대응, 생성 동일성, 전체 테스트·race·vet, 독립 서버 MCP 호출. `make codex-check`는 설치된 Codex 발견, `make cli-check`는 실제 PTY 계좌 선택/철회.
- 초기 R2 검증 당시 전체 테스트/하위 테스트 393개·race 393개·vet·모듈 합계·생성 동일성 통과, 같은 소스 바이너리 SHA-256 재현 확인. 모의 서버 강제 종료 후 stale credential 거부/재시작 회전까지 검증했다.
- 전체 명령·클라이언트 버전·결과 파일과 수용 기준별 근거는 [검증 기록](verification/README.md). 프로토콜 검증과 Codex 발견을 구분한다.

키 없는 사용은 `./bin/tossinvest-tools serve --backend=mock` → 다른 터미널의 `setup accounts --backend=mock` → MCP 등록/재연결이다. 초기 허용 계좌는 빈 집합이다. `serve --milestone=R1`로 35개 도구만 실행할 수 있고 R2가 기본이다. 설치한 기본 사용자 서비스는 없으며 모든 테스트 서버를 종료했다.

## CLI 조회 후속 구현 — 2026-10-26

- `accounts`, `holdings --account-alias=primary [--symbol=005930]`, `prices --symbols=005930,AAPL`를 추가했다. `--backend=toss|mock`, `--app-dir`, `--json`을 사용하며 기본 출력도 data/meta JSON이다. 계좌 별칭은 사용자가 선택한 값으로 바꾼다.
- `internal/runtime/query.go`가 기존 ConnectionCredential과 공식 MCP SDK로 실행 중인 서버의 고정 도구 세 개만 호출한다. CLI는 Toss 키/토큰 관리자나 계좌 정책을 소유하지 않는다. 서버가 없으면 exit 3이고 직접 접속 fallback은 없다. 인증/금지·철회 계좌는 4, 필수/부적절 필드 입력은 2, 준비 미완료는 5다.
- `internal/cli/query_test.go`, `internal/runtime/query_test.go`와 확장한 scripts/check-cli.py가 mock 조회·마스킹·권한 철회·wrong credential·서버 부재·필터/서버 계좌 헤더·upstream 오류·큰 정수/Decimal/null·비로컬 credential 전송 차단을 검증한다. 빈 symbol을 명시하면 전체 자산 조회로 바꾸지 않고 입력 오류로 거부한다.
- 이번 확장 전체 테스트/하위 테스트 399개·race 399개·vet·정적 검사 통과, 실패 0개. 빌드한 독립 CLI 프로세스에서도 세 조회·권한 철회·서버 부재를 검증했다.
- 현재 확장 빌드·검증은 [CLI 조회 검증](verification/cli-queries.md)과 [요약](verification/cli-queries/summary.json)을 따른다. 초기 R2의 기존 build.txt/summary.json은 당시 결과다. 실행 파일 bin/tossinvest-tools와 bin/SHA256SUMS는 이번 확장으로 갱신했다.
- REST/MCP 원본 계약·35/39개 도구 수·의존성·DB schema는 바꾸지 않았다. API 대응 데이터에 세 작업의 cli_command와 테스트 근거를 추가했다. 실제 Keychain·실계좌·운영 서비스는 이번에도 사용하지 않았다. 실제 조회는 사용자가 준비한 readonly 서버에 CLI를 실행해 확인해야 한다.

## 구현 경계와 주의점

- `internal/broker/toss/generated/client.gen.go`: 원본에서 생성한 전체 모델/client. 직접 수정 금지. typed readonly dispatcher도 scripts/generate-adapter.py로 생성한다.
- `internal/broker/toss`: 실제 auth/REST·single-flight·동적 그룹 limiter·origin/method/path 최종 readonly guard. 실제 WS는 core의 한 관리자에서 같은 Token 함수를 공유한다.
- `internal/broker/mock`: 실제 Keychain/외부 Toss 접속 없이 로컬 HTTP/WS wire를 제공한다. runtime mock는 실제 SecretStore/provider에 접근하지 않는다.
- `internal/core`: 모든 도구의 입력·scope·계좌·revision·ID 관계·결과 정책, WS full-replace/ACK/PING/복구·이벤트 큐·MAC cursor. 네트워크 중 DB transaction을 열지 않는다.
- `internal/store`: SQLite WAL/FULL·metadata/grants/관계·이벤트·sync/snapshot·VACUUM INTO 백업. 미래 T1 저널은 구현하지 않으며 prune은 events에 한정한다.
- `internal/runtime`: config·단일 인스턴스·동일 UID owner 소켓·인증 HTTP/MCP·helper 검증·종료 수명. `internal/platform`: native Keychain/peer UID/파일·launchd. `internal/cli`: 숨김 TTY setup·계좌 선택·진단·서비스·offline 백업/복원.

원본 예제의 ETF englishName 필수 선언 불일치, 응답의 새 필드/미지 enum/Decimal 보존, 동적 제한·계좌 마지막 전송 전 재검사, WS 복구 시 수신 버전 경합은 [adapter 기록](adapter-notes.md)을 따른다. 원본과 생성 코드에 수동 보정을 넣지 않았다.

계좌 별칭은 backend/client에 연결하며 다른 계좌로 재할당하지 않는다. 이름 변경은 기존 grant를 철회한 뒤 발견/선택한다. 60초 발견 세션과 revision을 검증하며 clear는 upstream 없이 적용한다. unknown 주문/조건 ID는 요청한 허용 계좌의 단건 GET만 하며 confirmed 타 계좌/conflict는 전송 전 거부한다.

R2 주문 이력은 7일·계좌당 100,000건·전체 payload 256 MiB, 큐는 1,024건·8 MiB·대기 1초, 메시지는 1 MiB다. 보존 prefix/watermark와 MAC cursor를 사용한다. 재시작·복원·revision 변경은 cursor 만료다. REST 복구는 현재 상태를 확인할 뿐 누락된 이력을 재생하지 않으며 gap은 유지한다. Keychain·파일·동일 UID 소켓을 에이전트 셸에 대한 독립 격리라고 설명하지 않는다.

## 사용 준비·시크릿 재점검 — 2026-10-26

- 에이전트 readonly 시험 사용과 실제 계좌 운용 검증 대기를 구분해 [준비 상태 점검](verification/readiness-audit/README.md)에 기록했다. 실제 키·Keychain·실계좌·운영 서비스를 사용하지 않았다.
- 검토 중 복구 REST의 권한 철회 후 후속 전송, 연결 여부로 freshness를 확정한 snapshot, CLI clear의 불필요한 upstream 발견을 찾아 수정했다. 공통 최종 dispatch guard·revision 게시 검사, stale=null/true, 발견 없는 clear에 회귀 테스트를 추가했다.
- `.gitignore`를 실행 credential/소켓/lock/로그/거절 DB까지 보강하고 Gitleaks v8.30.1 기반 `scripts/audit-secrets.py`와 `make audit-secrets`를 추가했다. 원본 전체 hash로 검증된 공개 예제와 알려진 go.sum checksum만 비시크릿으로 분류하며 키 값은 보고하지 않는다.
- 인덱스·작업 파일·미추적 publish 후보·모든 Git blob·커밋 이력을 각각 검사한다. 현재 인덱스에는 진입점/테스트가 빠져 있으므로 최종 파일의 스테이징이 필요하다. 이 점검에서 스테이징·커밋·푸시는 하지 않았다.
- 이번 재점검은 전체 테스트/하위 테스트 402개·race 402개·vet·정적 검사·독립 CLI/SDK/Codex mock 발견이 통과했다. 이미 실행 중인 실제 서버에는 사용자가 재시작/MCP 재연결해야 보완이 적용되며 이 점검에서 사용자 서버를 조작하지 않았다.
- 현재 실행 파일/검증은 [준비 상태 점검 요약](verification/readiness-audit/summary.json)을 따른다. 앞선 393/399개와 build/summary 파일은 각각 당시 기록이다.

## 바로 다음 작업

1. 사용자가 원하면 [로컬 운영](local-operation.md)의 mock MCP 예시를 등록하고 Restart한 뒤 이 채팅에서 상태/시장/허용 계좌 도구를 직접 호출한다. 현재 기록은 실제 Codex client의 별도 프로세스 발견까지이며 모델 직접 호출 완료로 확대하지 않는다.
2. 실제 연결을 원할 때만 사용자가 로컬 `setup --backend=toss`로 키를 숨김 입력하고 Keychain/IP/데이터 처리 범위를 준비한다. 에이전트가 실제 키를 읽어 출력하거나 대신 수집하지 않는다.
3. 사용자가 허용한 조회 범위에서 Keychain UX, 실제 계좌 헤더의 단건 제한, REST/WS·절전 복구를 검증한다. 확인 전 live-verified로 바꾸지 않는다.
4. 실제 운영 서비스 설치는 사용자가 명시적으로 요청/실행할 때 수행한다. 현재 plist 생성/명령 구현·모의 검증을 설치 성공으로 쓰지 않는다.
5. 주문이 필요하면 P4/T1을 별도 범위로 시작한다. 독립 승인·격리·저널·한도·멱등/미확정 복구 결정을 실제 설계 문서에서 읽고 구현한다. readonly R2를 전체 API 완료로 바꾸지 않는다.

## 최종 Git 반영 요청 — 2026-10-26

사용자의 명시 요청으로 최종 구현·문서·검증 기록을 모두 스테이징하고 인덱스 포함 시크릿 검사·정적 검사를 통과시킨 뒤 현재 main을 origin/main에 일반 푸시한다. 이전 절의 “커밋·푸시를 하지 않았다”는 각 검토 당시의 기록이다. 현재 커밋은 `git log -1`, 원격 반영은 `git status --short --branch`와 원격 main ref로 확인한다. 바이너리·.tools·credential·runtime DB는 .gitignore로 제외한다. 원본 명세의 공개 예제만 전체 고정 hash/정확한 행·규칙으로 구분하며, 커밋 이력 검사에서도 해당 커밋의 원본 blob hash를 확인한다. 실제 키·계좌·설치·주문·배포 검증 대기는 그대로 유지한다.
