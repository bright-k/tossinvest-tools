# 로컬 운영

readonly R2 0.2.0, macOS Apple Silicon 기준이다. 서버는 독립 실행하며 Codex는 로컬 HTTP MCP에 접속한다. 실행 파일에는 실제 Toss REST/WS 어댑터와 별도 mock이 포함된다. 실제 키·실계좌·서비스 설치는 이번 검증에서 사용하지 않았다. [검증 결과](verification/README.md)의 완료/대기 구분을 따른다.

## 빌드와 키 없는 실행

저장소에서 실행한다. Go·의존성은 `.tools`에만 준비하며 시스템 PATH나 전역 패키지를 변경하지 않는다. 재생성과 검증 스크립트는 Python 3 표준 라이브러리를 사용한다. macOS Command Line Tools의 clang은 Keychain 네이티브 연결 빌드에 필요하고, 실행에는 별도 언어 런타임이 필요 없다.

```sh
./scripts/bootstrap
make check check-generated test race vet build
./bin/tossinvest-tools version
./bin/tossinvest-tools serve --backend=mock
```

다른 터미널에서 모의 계좌를 선택하고 상태를 확인한다. 자동으로 모든 계좌를 허용하지 않는다.

```sh
./bin/tossinvest-tools setup accounts --backend=mock
./bin/tossinvest-tools status --backend=mock --json
./bin/tossinvest-tools doctor --backend=mock --json
```

선택 화면의 `1:primary` 같은 index:alias를 입력한 뒤 `YES`로 전체 허용 집합 교체를 확인한다. 철회는 아래와 같고 upstream 장애 중에도 가능하다.

```sh
./bin/tossinvest-tools setup accounts --backend=mock --clear
```

`serve --milestone=R1 --backend=mock`는 REST 34개+상태 35개 도구만 제공한다. 기본 R2는 39개다. `--mode=trading`은 toss/mock 모두 exit 2 / CAPABILITY_UNAVAILABLE이다. 환경변수·설정·이전 DB로 모드를 승격시키지 않는다.

## CLI 업무 조회

`accounts`, `holdings`, `prices`는 실행 중인 서버의 인증된 `/mcp`를 사용한다. CLI는 별도 Toss 토큰을 발급하거나 계좌 발견/grant를 자동 수행하지 않는다. 기존 서버를 그대로 사용할 수 있고 MCP 도구 수는 R1 35개/R2 39개로 유지된다.

```sh
./bin/tossinvest-tools accounts --backend=mock --json
./bin/tossinvest-tools holdings --account-alias=primary --backend=mock --json
./bin/tossinvest-tools holdings --account-alias=primary --symbol=005930 --backend=mock --json
./bin/tossinvest-tools prices --symbols=005930,AAPL --backend=mock --json
```

실제 서버에 연결하려면 `--backend=toss`로 바꾼다. 생략하면 비밀 없는 설정의 backend, 그 값이 없으면 toss를 따른다. 테스트는 임시 app-dir의 mock만 사용했다. 이 명령 안내를 실제 계좌 조회 검증으로 해석하지 않는다.

출력은 기본적으로 원본 MCP 결과의 `data`·`meta` JSON이며 `--json`도 받는다. 숫자·Decimal 문자열·null·추가 필드는 보존한다. holdings는 `--account-alias`가 필수이고 `--symbol`은 선택이다. prices는 최대 200개 콤마 구분 `--symbols`가 필수다. accountSeq/accountNo·토큰·임의 upstream URL 입력 옵션은 없다.

MCP와 동일한 주체/scope·별칭 grant·반환 직전 revision 검사를 거친다. 미선택 accounts는 `data=[]`와 `account_selection_required=true`, 금지/철회 계좌는 exit 4다. 입력 오류는 2, 서버 미실행은 3, 준비 미완료는 5, 원본 조회 오류는 상세 code와 함께 1이다. 업무·필드 검증 오류는 stderr JSON에만 쓰고 정상 stdout 데이터를 출력하지 않는다. 알 수 없는 CLI 옵션 등 파싱 오류는 기존 stderr 사용 안내와 exit 2를 따른다. [모의 검증 기록](verification/cli-queries.md)을 참고한다.

## Codex MCP 등록

다음은 사용자가 Codex 설정에 추가할 예시다. 이 작업에서 사용자 Codex 등록 파일을 바꾸지 않았다. 실행 파일 경로는 현재 저장소의 절대 경로이며 파일을 이동하면 수정한다.

```toml
[mcp_servers.tossinvest]
url = "http://127.0.0.1:17840/mcp"
http_headers_helper = "'/Users/bright/Desktop/work/tossinvest-tools/bin/tossinvest-tools' mcp-auth-header --backend=mock"
startup_timeout_sec = 20
tool_timeout_sec = 60
```

`http_headers_helper`는 설치된 Codex 0.164.0-alpha.11.1에서 **문자열 명령**으로 검증했다. command/args 객체 형식이 아니다. helper는 MCP 접속 credential만 클라이언트에 전달한다. 사용자가 helper를 수동 실행해 결과를 채팅에 복사할 필요가 없다. mock 재시작은 credential을 회전한다. 연결을 재시작하거나, Codex의 401/403 후 helper 갱신을 사용한다. 실행 파일·명령 자체에는 Toss 키를 넣지 않는다.

설정 저장 후 Codex의 MCP 서버 Restart 또는 새 연결에서 `tossinvest`가 연결되었는지 확인한다. 상태·시장 조회를 먼저 호출하고 계좌 조회에는 로컬에서 선택한 `account_alias`를 명시한다. 이 채팅 자체의 도구 등록 갱신과 모델의 직접 도구 호출은 사용자 재연결 후 확인해야 한다. 실제 설치된 Codex 클라이언트의 별도 프로세스 **도구 발견**은 검증했고, 대표 호출 전체는 공식 Go MCP 클라이언트에서 검증했다.

공식 설정 근거는 [Codex MCP 문서](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)다. 로컬 HTTP helper를 클라우드 실행 환경의 localhost 연결 보장으로 확대하지 않는다.

## 실제 Toss 연결: 사용자가 로컬에서 수행

1. 공식 WTS의 Open API 설정에서 본인의 client 자격증명과 실제 공인 출구 IP 허용 목록을 준비한다. VPN·공유기 변경 시 출구 IP를 확인한다. 데이터의 외부 모델 전달 범위와 이용 조건도 사용자가 결정한다.
2. mock 서버를 종료한다. 실제 서비스 설치나 키 입력은 아래 명령을 사용자가 직접 실행할 때만 발생한다.
3. 터미널에서 `./bin/tossinvest-tools setup --backend=toss`를 실행한다. client ID/secret은 숨김 TTY 입력으로 Keychain에 저장되며 채팅·명령 인자·환경변수에 넣지 않는다. 별도의 MCP credential을 생성한다. 재설정한 경우 서버와 MCP 연결을 재시작한다.
4. `./bin/tossinvest-tools serve --backend=toss`를 실행한다. Keychain은 로그인/잠금/접근 승인 상태에 따라 사용자 조작이 필요할 수 있다. MCP 항목이 없으면 setup 필요 오류로 시작을 거부하며, Toss 인증 실패 시에는 credential이 준비된 로컬 진단을 제공한다. mock로 자동 전환하지 않는다.
5. 다른 터미널에서 `./bin/tossinvest-tools setup accounts --backend=toss`로 마스킹 목록을 발견하고 계좌 별칭을 선택한다. CLI가 별도 토큰을 발급하지 않고 실행 중인 서버를 사용한다. `doctor --backend=toss --json`으로 준비 상태를 확인한다.
6. MCP 설정 helper의 `--backend=mock`를 `--backend=toss`로 바꾸고 연결을 재시작한다. 실제 단건 주문의 계좌 제한 전제, REST/WS 응답, 절전·네트워크 복귀는 허용한 범위에서 검증한 뒤에만 live-verified로 기록한다.

Keychain과 같은 UID 소켓은 동일 OS 사용자의 셸 접근에 대한 강한 격리가 아니다. 쓰기 모듈·승인은 제공하지 않는다.

## 경로·권한·진단

기본 앱 루트는 `~/Library/Application Support/tossinvest-tools`다. 테스트에는 `--app-dir=/private/tmp/짧은경로`를 쓴다. 소유자 소켓의 macOS 경로 한도를 넘으면 명확히 실패한다.

| 자료 | 위치/정책 |
|---|---|
| 비밀 없는 설정 | 앱 루트 `config.json`, 알 수 없는 mode/scope/grant 필드 거부 |
| 실제 DB·소유자 소켓 | `state.sqlite3`, `owner.sock` |
| mock DB·소유자 소켓·접속 파일 | `mock/state.sqlite3`, `mock/owner.sock`, `mock/connection.json` |
| 로그 | 기본 `~/Library/Logs/tossinvest-tools/server.jsonl`; 별도 app-dir은 그 아래 `logs/` |
| 로그 순환 | 시작/종료 기록, 파일 1 MiB 초과 시 최대 3개 이전 파일 |
| Toss Keychain service/account | `com.bright.tossinvest-tools.toss-client` / `local-user` |
| MCP Keychain service/account | `com.bright.tossinvest-tools.mcp.codex-readonly` / `local-user` |

앱 디렉터리 0700, credential/DB/소켓 0600이다. helper는 소유 UID·권한·symlink·backend·run_id와 살아 있는 소유자 소켓을 검사한다. backend와 무관하게 앱 루트당 한 서버만 허용한다. `/healthz`는 비밀 없는 생존만, `/admin/v1/status`와 `/mcp`는 매 요청 인증을 요구한다. owner 라우트는 TCP에 없다.

상태의 query_ready·streams_ready·trading_ready를 따로 본다. readonly에서 trading_ready=false는 정상이다. 서버가 없으면 CLI exit 3, 인증/권한 거부 4, 준비 미완료 5다. 일반 실패 1, 입력·설정·trading 미지원 2, 결과 미확정 6을 사용한다. 상태는 JSON이며 `--json`도 지원한다. doctor는 토큰을 독립 재발급하거나 수리하지 않는다. R2의 실시간 기능이 미준비인 부분 진단도 exit 5이며, R1의 정상적인 streams_ready=false는 실패로 취급하지 않는다.

## 실시간과 이력

```json
{"subscriptions":[{"channel":"trade:us","codes":["AAPL"]},{"channel":"orderbook:kr","codes":["005930"]}]}
```

`tossinvest_set_subscriptions`의 위 입력은 자신의 시장 watchlist 전체 교체다. 빈 배열은 시장 watchlist를 비운다. 현재 허용 계좌의 필수 주문 구독은 서버가 합친다. personal:order의 codes를 명시할 때도 원본 accountSeq가 아닌 허용 **별칭**을 사용한다. 채널×종목+필수 계좌 합계 100개를 제한하며 선언은 최소 210ms 간격이다.

`tossinvest_get_stream_snapshot`은 channel/symbol의 최신 관측 스냅샷, `tossinvest_get_stream_status`는 연결·거부·동기화 상태를 반환한다. `tossinvest_list_order_events`는 필수 account_alias와 선택 limit(1~1000)/cursor로 커밋된 수신 이력을 읽는다. 7일·100,000건/계좌·전체 payload 256 MiB 한도를 적용한다. 1,024건·8 MiB·1초 대기 또는 메시지 1 MiB 초과/저장 실패는 gap·재연결·겹침 REST 복구로 처리한다.

재시작·복원·권한 revision 변경과 보존 범위 이전 cursor는 CURSOR_EXPIRED다. cursor 없이 보존 범위부터 다시 읽고 REST로 현재 상태를 확인한다. REST 복구는 누락된 이벤트를 복원하지 않으며 `sync_status=synced`와 `gap_detected=true`가 함께 있을 수 있다. 앱/WTS 동시 주문과 공식 API에서 보이지 않는 주문 유형 때문에 계좌 전체 원장으로 간주하지 않는다.

## 선택적 사용자 서비스

이 작업에서는 설치하지 않았다. 사용자가 원할 때 절대 실행 경로를 고정한 LaunchAgent를 설치한다. 기존 plist는 자동 덮어쓰지 않고 명시적 uninstall 후 재설치를 요구한다.

```sh
./bin/tossinvest-tools service install --backend=toss --dry-run
./bin/tossinvest-tools service install --backend=toss
./bin/tossinvest-tools service status --backend=toss
./bin/tossinvest-tools service stop --backend=toss
./bin/tossinvest-tools service uninstall --backend=toss
```

plist는 readonly argv·0700/0600 수명·umask 077·30초 재시작 제한을 사용한다. 등록 상태와 실제 실행 서버를 구분한다. stop/uninstall은 계좌 주문 취소가 아니며 DB·Keychain을 지우지 않는다. 로그인 사용자 서비스이므로 로그아웃 후 지속 실행을 보장하지 않는다. 실제 launchd 설치/제거·Keychain 잠금·절전 UX는 사용자 opt-in 운영 검증 대기다.

## 백업과 복구

서버를 종료한 뒤 실행한다. 실행 중 백업/복원은 단일 인스턴스 잠금으로 거부한다. 백업은 SQLite VACUUM INTO로 일관된 복사본을 만들고 0600을 적용한다. WAL을 무시한 실행 중 DB 단독 복사를 사용하지 않는다.

```sh
./bin/tossinvest-tools backup --backend=mock --output=/private/tmp/toss-backup.sqlite3
./bin/tossinvest-tools restore --backend=mock --app-dir=/private/tmp/toss-restored --input=/private/tmp/toss-backup.sqlite3
```

restore는 기존 DB를 덮어쓰지 않는다. 기존 DB/WAL을 별도로 안전하게 보관하고 빈 대상에 복원한다. backend 불일치·손상·미지원 schema 버전은 거부한다. Keychain은 DB 백업에 포함하지 않는다. 복원 후 이전 cursor는 만료되고 새 구독·REST 동기화가 필요하다. 승인·미확정 전송 등 미래 T1 테이블에 이벤트 prune을 적용하지 않는다.
