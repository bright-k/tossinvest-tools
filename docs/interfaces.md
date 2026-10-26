# 접속과 결과 계약

readonly R1/R2에서 구현한 최소 인터페이스입니다. 실제 명령·검증 범위와 사용자 설정 대기는 [로컬 운영](local-operation.md)·[검증 기록](verification/README.md)을 따릅니다. mode와 backend는 [실행 프로필](runtime-profiles.md), 원본 업무 입력은 고정 명세와 [API 대응 데이터](api/operations.json)를 따릅니다. 정확한 CLI 도움말과 JSON schema는 이 기준을 P1·P2에서 테스트로 고정합니다.

## 초기 CLI 범위

| 명령 | 역할 |
|---|---|
| `setup` | 비밀 없는 설정, 사용자가 로컬에서 등록하는 토스 키, 별도의 최소 권한 MCP 접속 자격증명 준비 |
| `setup accounts` | 실행 중인 서버에서 계좌를 발견하고 소유자가 별칭·접속 주체별 허용 계좌를 선택. 로컬 소유자 소켓만 사용 |
| `serve` | `--mode=readonly|trading`, `--backend=toss|mock`로 독립 서버 실행 |
| `doctor` | 로컬 설정과 실행 서버를 읽기 진단. 토스 토큰을 독립 재발급하지 않음 |
| `accounts` | 기존 MCP 인증/계좌 정책으로 허용 마스킹 계좌 목록 조회 |
| `holdings --account-alias=<alias> [--symbol=<symbol>]` | 허용 별칭의 보유 자산 조회 |
| `prices --symbols=<symbols>` | 최대 200개 콤마 구분 심볼의 현재가 조회 |
| `status` | 실행 서버의 mode/backend·준비·capability를 확인 |
| `mcp-auth-header` | 로컬 MCP 클라이언트 전용 인증 helper. 등록된 제한적 접속 헤더만 기계 판독 JSON으로 반환 |
| `service install/status/stop/uninstall` | 명시적으로 선택한 사용자 서비스 수명 관리 |

34개 원본 조회의 기본 업무 인터페이스는 MCP이며, 후속 요청으로 계좌 목록·보유 자산·현재가 3개에 CLI를 추가했습니다. CLI는 같은 `/mcp`를 고정 도구 이름으로 호출하고 기존 접속 credential provider를 사용합니다. 출력은 기본/--json 모두 서버의 data/meta JSON, 업무 실패는 기존 오류 code를 보존한 stderr JSON과 기존 exit code입니다. credential은 메모리에만 전달하며 redirect/다른 origin 전송을 거부합니다. 알 수 없는 CLI 옵션 등 flag 파싱 오류는 기존 stderr 사용 안내와 exit 2를 유지합니다. 조회용 CLI 전체를 R1 종료 조건에 암묵적으로 포함하지 않습니다. 후속 업무 CLI를 추가하면 같은 서버와 정책을 사용하며 토스 직접 연결을 만들지 않습니다.

일반 진단·상태 명령은 사람이 읽는 출력과 `--json`을 제공합니다. 상태를 출력하는 것만으로 토스 비밀이 노출되지 않아야 합니다. 인증 helper는 일반 진단 출력과 분리하고, 출력에는 토스 키·토스 access token·소유자 승인 자격증명을 넣지 않습니다. helper에 사용할 자격증명은 서비스 실행 권한과 다른 MCP 접속용 최소 권한 credential입니다. 에이전트가 helper를 수동 실행해 인증값을 채팅으로 가져오는 흐름을 만들지 않습니다.

예정 exit code는 0 성공, 1 일반 실행 오류, 2 인자·설정 오류, 3 서버 접속 불가, 4 인증·권한 거부, 5 준비 미완료, 6 작업 결과 미확정입니다. 종료 코드를 오류 내용의 유일한 구분으로 삼지 않고 JSON 오류 코드를 함께 제공합니다. 부분 진단 실패는 성공처럼 표시하지 않습니다.

## 로컬 경로 기준

사용자 홈 경로는 OS에서 해석하고 하드코딩하지 않습니다. 기본 앱 디렉터리 이름은 `tossinvest-tools`입니다.

| 자료 | macOS 기준 |
|---|---|
| 비밀 없는 설정 | 사용자 Library의 `Application Support/tossinvest-tools/config.json` |
| SQLite | 같은 앱 디렉터리의 `state.sqlite3` |
| 서버 로그 | 사용자 Library의 `Logs/tossinvest-tools/server.jsonl`과 순환 파일 |
| Keychain 참조 | 토스 client 자격증명과 MCP connection 자격증명을 별도 service/account 이름으로 구분 |
| 소유자 제어 소켓 | 앱 디렉터리의 `owner.sock`. TCP HTTP·MCP로 노출하지 않음 |
| mock 상태 | 앱 디렉터리의 `mock/state.sqlite3`, `mock/owner.sock`, 실행 중에만 유효한 `mock/connection.json` |

디렉터리 0700·민감 파일 0600을 기본으로 검증합니다. 테스트는 임시 디렉터리와 fake SecretStore를 주입합니다. 운영 경로·파일 형식을 바꾸면 설정·DB 이전 계약을 갱신합니다. readonly 초기 DB에는 schema metadata와 계좌 별칭 등 필요한 상태만 두고, trading 저널은 후속 migration으로 추가합니다.

## 최초 연결 흐름

1. 실제 연결은 사용자가 로컬 `setup`에서 키를 숨김 입력하고 Keychain에 저장합니다. 별도의 MCP 접속 credential도 준비하지만 허용 계좌는 빈 집합으로 시작합니다. setup은 trading을 활성화하지 않습니다.
2. 서버를 readonly로 시작합니다. 아직 계좌를 선택하지 않았어도 인증된 상태·시장 조회는 사용할 수 있으며, 계좌별 준비에 `ACCOUNT_SELECTION_REQUIRED`를 표시합니다.
3. 사용자가 별도 터미널에서 `setup accounts`를 실행하여 서버가 발견한 계좌의 마스킹 표시를 보고 별칭과 접속 주체별 허용 계좌를 선택합니다. 서버가 매핑을 커밋한 뒤 해당 계좌 조회가 가능해집니다.
4. MCP 클라이언트는 `mcp-auth-header`로 별도 MCP credential만 읽습니다. helper의 Keychain service/account 참조는 토스 credential과 분리·고정하며 비밀 저장·실패의 인자·로그 비노출을 검증합니다.
5. 모의 실행은 `serve --backend=mock`만으로 키 없는 인증된 서버를 준비합니다. 계좌 선택은 `setup accounts --backend=mock`, helper는 `mcp-auth-header --backend=mock`를 사용하며 아래 mock 계약을 따릅니다. 실제 SecretStore·Keychain·외부 네트워크는 호출하지 않습니다.
6. Codex의 로컬 HTTP MCP 등록 예시는 실제 클라이언트 호환 검증 뒤 고정합니다. GUI가 셸 환경변수를 상속한다고 가정하지 않습니다.

토스 키가 없으면 실제 조회는 준비되지 않았다고 표시하고 mock로 자동 전환하지 않습니다. 실제 키 등록·실계좌 조회는 사용자가 허용한 연결 절차에서 수행합니다. 이 구현 문서 정리만으로 실제 계좌 접속을 실행하지 않습니다.

## 계좌 선택과 접속 권한

`setup accounts`는 소유자 전용 Unix domain socket에 접속합니다. 서버는 소켓 디렉터리 0700·소켓 0600과 OS peer UID가 서버 실행 UID와 같은지 검사합니다. CLI는 대화형 TTY에서 선택을 받아 전송하며 원본 계좌 식별자를 명령 인자로 받지 않습니다. 같은 UID의 셸 접근까지 격리하는 수단은 아니며, 실주문 승인의 독립 인증을 대체하지 않습니다. 서버가 없으면 exit 3으로 실패하고 CLI가 토스에 직접 접속하지 않습니다.

소켓은 HTTP over Unix socket으로 `GET /owner/v1/status`, `POST /owner/v1/accounts/discover`, `PUT /owner/v1/accounts/grant`만 제공합니다. TCP listener에는 이 라우트를 등록하지 않습니다. `setup accounts --principal=<id>`의 기본 주체는 codex-readonly입니다. grant 입력은 발견 세션 ID·expected_access_revision·principal_id·선택한 handle/alias 배열이며, 그 주체의 허용 계좌 집합을 원자적으로 교체합니다. 빈 배열은 전부 철회이고, 다른 주체의 허용은 유지합니다. 성공 전 최종 선택 내용을 TTY에서 확인하며 사용자 키·credential·accountSeq를 이 입력에 넣지 않습니다.

서버가 중앙 토큰 관리자로 accounts를 조회하고, 60초 유효한 발견 세션 안에서만 선택 가능한 임시 handle·계좌 유형·번호 끝 4자리의 마스킹 표시를 CLI에 제공합니다. 목록은 모델 도구에 반환하지 않습니다. 선택 시 사용자가 별칭을 지정하며 기본 제안은 `account-1`처럼 번호와 무관한 이름입니다. 별칭은 `[a-z][a-z0-9_-]{0,31}`이고 backend 안에서 유일하며, 다른 계좌에 기존 별칭을 재할당하지 않습니다. 계좌 재등록·별칭 변경은 기존 허용 관계를 폐기한 뒤 명시적으로 적용합니다.

서버는 backend·토스 client 참조·accountSeq·별칭·주체별 허용 관계·`access_revision`을 SQLite에 저장합니다. 원본 accountNo는 발견 처리 후 보관하지 않고 끝 4자리만 저장합니다. 계좌 매핑과 권한 변경은 한 트랜잭션으로 적용합니다. 발견 세션 만료·기준 revision 충돌·목록에서 사라진 계좌는 적용하지 않고 다시 발견하도록 합니다. 자산·주문을 자동으로 조회하거나 모든 계좌를 자동 허용하지 않습니다.

토스 client 신원이 바뀌면 이전 client의 계좌 grant·ID 관계를 새 client에 적용하지 않고 재선택을 요구합니다. 알 수 없는 principal_id는 INVALID_ARGUMENT로 거부하며 grant 요청으로 주체를 자동 생성하지 않습니다.

grant 적용 전 서버가 accounts를 다시 조회해 선택 계좌의 존재를 확인하고, 네트워크 호출을 마친 뒤 revision을 재검사해 DB에 커밋합니다. 네트워크 대기 중 DB 트랜잭션을 열어 두지 않습니다. 실패하면 기존 허용 집합을 유지하며 부분 적용하지 않습니다. 입력 오류는 INVALID_ARGUMENT, 만료·revision 충돌은 CONFIG_CONFLICT로 처리하고 CLI는 exit 2와 재선택 안내를 제공합니다.

전부 철회는 `setup accounts --clear`로 수행하며 TTY에서 확인합니다. 이 요청은 선택 배열을 비우고 발견 세션 ID는 null로 보내며, 로컬 revision 검사만으로 적용합니다. upstream 장애나 토큰 만료 때문에 권한 철회가 막히지 않도록 계좌 재조회와 발견 세션을 요구하지 않습니다. 개별 제거도 기존 허용 집합의 부분집합만 남기는 경우에는 같은 로컬 철회 원칙을 적용하고, 새 계좌 추가·별칭 변경에는 발견 절차를 요구합니다.

초기 MCP 주체는 `codex-readonly` 하나입니다. setup은 아래 조회 scope를 갖는 접속 credential을 준비하고, 서버는 첫 시작에서 해시·주체를 등록하되 허용 계좌는 빈 집합으로 둡니다. 계좌 허용 집합은 사용자가 선택할 때만 채웁니다. 읽기 주체에 주문·소유자 제어 scope를 추가하지 않습니다. 초기 버전은 다른 주체를 자동 생성하지 않으며 다중 주체 등록은 소유자 전용 경로를 추가하는 후속 확장입니다.

| scope | 허용 기능 |
|---|---|
| `status:read` | 서버 상태·인증된 진단 |
| `market:read` | 계좌 컨텍스트 없는 REST 조회 중 accounts를 제외한 시장 정보 |
| `accounts:read` | 허용 계좌 목록과 해당 계좌의 자산·주문·가능 금액 조회 |
| `streams:read` | R2 스트림 상태·시장 스냅샷. 주문 이벤트에는 accounts:read와 해당 계좌 권한도 필요 |
| `streams:manage` | R2 자신의 시장 watchlist 변경. 개인 주문 구독에는 accounts:read와 해당 계좌 권한도 필요 |

scope와 계좌 허용은 매 요청·상태 반환·이벤트 페이지에 다시 적용합니다. 권한 변경 시 `access_revision`을 증가시키고, 철회된 계좌의 새 전송·구독을 막으며 in-flight 결과도 반환 직전에 재검사합니다. 이미 전송한 조회를 철회할 수는 없습니다. 다른 주체의 watchlist를 변경하지 못하고, 필수 주문 구독은 서버가 현재 허용 계좌들의 합집합으로 관리합니다. 철회 후 기존 캐시·이벤트도 해당 주체에게 반환하지 않습니다.

`tossinvest_get_accounts`는 허용된 계좌만 `account_alias`, `account_type`, `account_no_masked`로 반환합니다. accountNo·accountSeq는 결과에서 제외하며 미지의 accountType 문자열은 보존합니다. 허용 집합이 비어 있으면 upstream 호출 없이 `data=[]`, `meta.account_selection_required=true`, `meta.source=local_policy`를 반환합니다. 이는 허용 계좌가 없다는 뜻이며 실제 계좌가 없다는 판정이 아닙니다. 허용 집합이 있는 조회의 upstream 실패는 오류로 반환하고 빈 배열로 대체하지 않습니다. 계좌가 사라지면 허용을 비활성화하고 재선택을 요구합니다.

## 주문 ID와 계좌 소유 관계

단건 주문·조건주문 조회에는 항상 `account_alias`가 필요합니다. 별칭 생략 시 첫 계좌를 선택하거나 ID만으로 다른 계좌를 탐색하지 않습니다. 로컬 관계의 키는 backend·토스 client 참조·자원 종류·주문 ID이며, 상태는 `unknown`, `confirmed(account)`, `conflict`로 구분합니다.

1. scope·별칭·계좌 허용을 확인하고, 확인된 타 계좌 관계나 conflict가 있으면 upstream 호출 전에 `PERMISSION_DENIED`로 거부합니다. 오류로 다른 계좌의 존재를 밝히지 않습니다.
2. unknown은 캐시 부재만으로 거부하지 않습니다. 요청한 허용 계좌의 `X-Tossinvest-Account`를 서버가 주입한 해당 단건 GET만 허용합니다. 다른 계좌 헤더·계좌 미지정·목록 전체 스캔으로 재시도하지 않습니다.
3. 계좌 컨텍스트에 제한된 성공 응답이나 해당 계좌 목록·인증된 개인 이벤트에서 확인한 ID만 관계를 확정합니다. 404는 이 계좌에서 `RESOURCE_NOT_FOUND`로 반환하며 계좌 전체의 부재·다른 계좌 소유·주문 종료를 추정하지 않습니다. 401·403·429·timeout·5xx는 원래 오류로 유지하고 소유 관계를 만들지 않습니다.
4. 다른 계좌의 성공 증거와 충돌하면 `UPSTREAM_DATA_INVALID`로 결과를 격리하고 이 ID의 추가 조회·쓰기를 막습니다. P4의 쓰기는 fresh 계좌 상태·확정된 대상 관계·별도 승인을 모두 요구하며 unknown 조회 허용을 쓰기 허용으로 확대하지 않습니다.

계좌 헤더에 따른 upstream 자원 제한은 broker의 명시적인 전제입니다. R1 mock에서 교차 계좌를 검증하고 사용자가 허용한 실제 조회 검증에서도 확인합니다. 실제 동작이 전제와 다르면 해당 단건 기능을 준비 미완료로 차단하고, 허용 계좌 목록에서 확인한 ID만 조회하는 보수적 adapter로 전환해 계약 변경을 기록합니다. 원본 API에 없는 소유 필드나 404의 추가 의미를 가정하지 않습니다.

## mock 접속 credential

토스 SecretStore와 MCP credential provider는 별도 interface입니다. toss에서는 두 종류를 Keychain의 서로 다른 항목으로 관리합니다. mock에서는 둘 다 실제 Keychain을 사용하지 않으며 토스 adapter는 고정 모의 데이터만 읽습니다. 로컬 HTTP의 인증·scope·Host·Origin 검증은 mock에서도 동일하게 적용합니다.

mock 서버는 매 시작 시 OS 난수 32바이트 이상의 bearer credential과 무작위 `run_id`를 새로 생성하고 메모리에 보관합니다. `mock/connection.json`에는 backend·run_id·listen·credential을 0600으로 원자적으로 기록하고 부모는 0700으로 둡니다. 이는 모의 서버 접속용 파일의 명시적 예외이며 토스 키·실제 MCP credential을 파일로 우회 저장하는 근거가 아닙니다. SQLite에는 credential의 해시와 주체 연결만 기록하며 평문을 넣지 않습니다.

`mcp-auth-header --backend=mock`는 이 고정 경로의 소유 UID·권한·일반 파일 여부와 symlink 부재를 확인하고, mock 제어 소켓에서 현재 run_id·backend가 일치하는지 확인한 뒤 MCP용 헤더 JSON만 stdout으로 반환합니다. 실제 backend helper는 이 파일을 읽지 않습니다. 인증·상태 helper가 backend를 자동 탐색하거나 다른 backend의 credential로 재시도하지 않습니다.

credential은 해당 프로세스 수명 동안만 유효합니다. 정상 종료에서는 파일을 제거하고 강제 종료 뒤 남은 파일은 다음 시작에서 교체합니다. 이전 credential·다른 run_id·toss credential은 mock에서 거부하고, mock credential은 toss에서 거부합니다. 파일·소켓이 없으면 서버 접속 불가, UID/권한 불일치는 권한 오류로 종료하며 무인증 fallback은 없습니다. 한 사용자 앱 루트의 서버는 backend와 무관하게 하나만 실행합니다. 테스트는 임시 앱 루트·fake provider로 격리합니다.

## 주문 이벤트 페이지와 커서

R2의 `tossinvest_list_order_events` 입력은 필수 `account_alias`, 선택 `cursor`, `limit`(기본 100, 1~1000)입니다. 한 번에 한 계좌의 커밋된 수신 이벤트만 조회하고 수신 순서대로 반환합니다. data는 `events`, `next_cursor`, `has_more`이며, 반환 시점의 권한과 [보존·수신 큐 계약](architecture.md#이벤트-보존과-수신-큐)을 적용합니다.

커서는 backend·run_id·주체·access_revision·계좌·마지막 수신 순서를 묶어 서버가 MAC으로 보호하는 불투명 문자열입니다. 숫자 순서값을 직접 받지 않습니다. 프로세스 재시작·권한 revision 변경·DB 복원으로 새 run_id가 생기면 이전 커서는 만료됩니다. DB 이벤트는 남아 있어도 커서 유효성을 재사용하지 않으며 중복 재조회 가능성을 명시합니다.

커서가 없으면 현재 보존된 가장 오래된 이벤트부터 시작합니다. `next_cursor`는 마지막 반환 위치이며, 빈 페이지에도 현재 위치의 커서를 반환해 이후 polling에 사용할 수 있게 합니다. 첫 빈 페이지는 해당 계좌의 현재 저장 high-watermark를 사용합니다. `has_more`는 조회 시점에 더 저장된 행이 있는지 나타내며 upstream의 완전성을 의미하지 않습니다.

오래된 이벤트를 삭제할 때 계좌별 `pruned_through_seq`를 같은 트랜잭션에 기록합니다. cursor 위치가 이 값보다 작으면 `CURSOR_EXPIRED`(retryable=false)를 반환하며 정상 빈 페이지로 바꾸지 않습니다. 만료 오류에는 권한이 있는 계좌의 `retained_from_at`·`restart_required=true`를 제공하고, 클라이언트는 cursor 없이 보존 범위부터 재조회한 뒤 REST로 현재 상태를 확인합니다. restart_required는 이벤트 조회를 처음부터 다시 시작하라는 뜻이며 서버 재시작을 요구하지 않습니다. 토큰 위조·형식 오류·미래 위치는 `INVALID_ARGUMENT`, 계좌/scope 철회는 먼저 `PERMISSION_DENIED`입니다.

페이지 meta에는 `retained_from_at`(없으면 null), `history_truncated`, `gap_detected`, `sync_status`, `last_synced_at`을 포함합니다. 보존 삭제와 WS 관측 누락은 별개입니다. REST 동기화 완료는 현재 상태를 복구할 뿐 사라진 이벤트를 복원하지 않으므로 `sync_status=synced`와 과거 누락의 `gap_detected=true`가 함께 존재할 수 있습니다.

## 최소 HTTP와 MCP 계약

`GET /healthz`는 Host·Origin 검증을 거쳐 비밀 없는 생존 여부만 반환합니다. `GET /admin/v1/status`는 인증 뒤 상태를 반환합니다. `/mcp`는 인증된 Streamable HTTP이며 프로토콜의 method·session을 업무 권한으로 해석하지 않습니다.

R1의 MCP에는 조회 도구 34개와 `tossinvest_get_server_status`를 제공합니다. R2에서 실시간 추가 도구 4개를 더합니다. 각 접속 주체의 scope로 도구 목록을 더 제한할 수 있습니다. 제공된 도구를 권한 없는 주체가 이름으로 호출해도 거부합니다.

조회 도구의 path/query 입력과 `data`의 업무 형식은 원본 schema를 기준으로 합니다. 서버가 처리하는 계좌·인증·멱등 필드는 직접 입력에서 분리합니다. 계좌 식별자 등 마스킹 변환은 명시하고, 원본의 통화·Decimal 문자열·시각·null과 페이지 정보를 임의로 없애지 않습니다.

공통 결과는 `data`와 `meta`를 사용하고 MCP structuredContent에 제공합니다. text 요약이 structuredContent와 다른 사실을 말하지 않게 합니다. `meta.schema_version`, `mode`, `backend`, `fetched_at`은 필수입니다. 데이터 기준 시각은 `data_timestamp`로 구분하고 원본에 없으면 null을 사용합니다. `stale`은 판단할 수 없으면 null이며, 계좌·실시간 응답은 관련 `sync_status`와 `gap_detected`를 포함합니다. 조회 오류를 성공 data의 0이나 빈 배열로 대체하지 않습니다.

서버 상태 data는 실행 중 mode/backend, 구현 capability와 이 주체에게 활성화된 capability, query_ready·streams_ready·trading_ready와 관련 제한 사유를 제공합니다. 상세 주체·계좌 정보는 권한에 맞게 별칭으로 제한합니다. 서비스 등록의 mode와 실행 프로세스의 mode는 별도 필드입니다.

## 오류 계약

| 로컬 코드 | 의미 |
|---|---|
| `INVALID_ARGUMENT` / `CONFIG_INVALID` | 입력 또는 시작 설정을 고쳐야 함 |
| `CONFIG_CONFLICT` | 계좌 선택 세션 만료·기준 revision 변경으로 재선택 필요 |
| `AUTH_REQUIRED` / `PERMISSION_DENIED` | 인증 또는 계좌·기능 권한 부족 |
| `NOT_READY` | 조회·연결 등 필요한 기능 미준비 |
| `READONLY_VIOLATION` | readonly 프로필에서 실제 쓰기 경로를 요청함 |
| `CAPABILITY_UNAVAILABLE` / `TRADING_NOT_READY` | 시작 시 요청한 거래 기능 미구현 또는 거래 준비 조건 미충족 |
| `UPSTREAM_RATE_LIMITED` / `UPSTREAM_ERROR` | 원본 오류·제한을 보존해 전달 |
| `OUTCOME_UNKNOWN` | 쓰기가 반영됐을 가능성이 있어 확인이 필요함 |
| `RESOURCE_NOT_FOUND` | 지정한 허용 계좌에서 자원을 찾지 못함. 다른 계좌 소유 여부는 밝히지 않음 |
| `UPSTREAM_DATA_INVALID` | 계좌 관계 충돌 등 응답 의미를 안전하게 확정할 수 없음 |
| `CURSOR_EXPIRED` | 보존 범위·프로세스·권한 revision이 바뀌어 이벤트 재조회가 필요함 |

업무 오류에는 code·message·retryable·correlation_id와 필요 시 upstream_code를 제공합니다. 쓰기 관련 오류에는 `broker_write_outcome`을 not_attempted·unknown·confirmed 중 하나로 표시합니다. retryable은 새 주문 ID를 만들어 재시도하라는 뜻이 아닙니다. 실제 전송 재시도는 기록된 작업과 프로필을 다시 검증합니다.

HTTP 인증 실패·JSON-RPC method 미등록 같은 transport 오류는 해당 프로토콜 오류로 유지합니다. 등록된 도구의 업무 실패는 MCP `isError=true`와 구조화한 오류를 사용합니다. 숨겨진 도구 직접 호출을 모두 성공 응답 안의 정책 메시지로 처리하지 않습니다.

## 준비 상태 재검토 — 2026-10-26

복구 중 내부 REST도 정상 도구와 같은 전송 직전 계좌 권한/revision 검사를 적용하며 snapshot 게시 전에도 재검사한다. 시세 freshness 정책이 없는 현재 연결 중 snapshot의 stale은 null이고 단절 후에는 true다. setup accounts --clear는 upstream 발견을 호출하지 않는다. [준비 상태 재점검](verification/readiness-audit/README.md)의 회귀 테스트를 따른다.
