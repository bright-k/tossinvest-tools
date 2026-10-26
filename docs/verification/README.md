# readonly R1/R2 검증 기록

가장 최근 코드/보안 보완과 실행 파일은 [준비 상태·시크릿 재점검](readiness-audit/README.md)을 따른다. 기존 결과 파일은 각각 당시 기록이다.

계좌 목록·보유 자산·현재가 CLI를 추가한 후속 빌드와 검증은 [CLI 조회 확장](cli-queries.md)을 따른다. 이 페이지의 393개/기존 build.txt·summary.json은 확장 전 R2 검증 기록이다.

기준일 2026-10-26 Asia/Seoul. macOS ARM64, Go 1.27.3, Python 3.9.6에서 검증했다. 실제 키 등록·실계좌 조회·서비스 설치·주문·push·배포는 수행하지 않았다.

## 판정 구분

| 항목 | 결과 |
|---|---|
| 구현 | readonly R1/R2 0.2.0. 실제 REST/WS adapter·mock·MCP·최소 CLI·Keychain adapter·계좌 권한·이벤트 복구/보존·로컬 서비스/백업 명령 |
| 고정 원본·모의 수용 | 아래 테스트·재생성·정적 검증 통과. 원본 REST/WS JSON은 변경 없음 |
| 독립 서버 프로토콜 | 공식 Go MCP SDK v1.8.0이 빌드한 별도 서버 프로세스에 인증 연결. R1 35개/R2 39개 발견, 각각 REST 조회 34개 전부 호출, R2 4개 대표 호출 |
| 설치된 Codex 클라이언트 | codex-cli 0.164.0-alpha.11.1 app-server 실제 MCP client에서 helper 인증과 35/39개 발견 통과 |
| 이 채팅/데스크톱 모델 호출 | 지속 MCP 등록을 수정하지 않았고 새 사용자 채팅/모델 턴도 생성하지 않음. 사용자가 등록·Restart 후 모델 직접 호출 확인 필요 |
| 실제 운영 | Keychain OS 등록/잠금 UX, 실제 Toss 출구 IP·계좌 헤더 제한·REST/WS·절전/네트워크 복귀·launchd 설치/제거는 사용자 opt-in 검증 대기 |
| 주문 | 이번 범위 제외. 6개 쓰기와 승인 실행은 planned. mock/toss trading 시작 모두 CAPABILITY_UNAVAILABLE, 전송 차단은 검증 |

`verified`는 offline 계약 검증이다. live-verified 항목은 0개다. 전체 공식 API 완료나 실계좌 운용 보증으로 해석하지 않는다. 실제 계좌 헤더가 단건 조회를 계좌에 제한한다는 전제는 별도 허용된 live 조회로 확인해야 한다.

## 최종 실행 요약

일반 테스트와 race detector에서 각각 테스트/하위 테스트 393개, 테스트가 있는 패키지 7개가 통과했고 실패는 0개다. vet·모듈 합계·생성 동일성도 통과했다. 최종 바이너리의 같은 소스 재빌드는 SHA-256이 동일했다. 전체 기계 판독 요약은 [summary.json](summary.json)이다.

## 재현 명령

```sh
./scripts/bootstrap
make check
make check-generated
./scripts/go test ./...
./scripts/go test -race ./...
./scripts/go vet ./...
./scripts/go mod verify
make build
make mcp-check
make codex-check
make cli-check
```

`make verify`는 check·check-generated·test·race·vet·mcp-check를 묶는다. codex-check는 설치된 Codex가 있을 때 별도로 실행한다. 기본 테스트는 로컬 httptest/WS·임시 SQLite·fake SecretStore만 사용한다. 샌드박스가 localhost bind도 막는 환경에서는 로컬 네트워크 실행 허용이 필요하며, 외부 Toss 인증/조회는 하지 않는다. 실제 Keychain/launchd 테스트는 기본 명령에 없다.

Go 다운로드 SHA-256과 버전은 [bootstrap](../../scripts/bootstrap), 생성기/라이브러리는 [go.mod](../../go.mod)·[go.sum](../../go.sum)에 고정했다. 생성은 [원본](../../specs/tossinvest/openapi.json)에서 직접 실행하며 [호환 처리](../adapter-notes.md)는 원본 밖에 있다. 실행 중 원본이나 도구를 갱신하지 않는다.

## 결과 파일

| 파일 | 의미 |
|---|---|
| [summary.json](summary.json) | 최종 빌드 SHA-256·수용/클라이언트 결과와 사용자 확인 대기 |
| [tests.jsonl](tests.jsonl) | 전체 go test -json 결과. 성공/실패·세부 테스트 이름 |
| [race.jsonl](race.jsonl) | 전체 race detector 결과 |
| [vet.txt](vet.txt) | go vet 출력. 통과 시 빈 파일 |
| [modules.txt](modules.txt) | 고정 모듈 합계 검증 |
| [specs.json](specs.json) | 원본 해시·ref·41/10 대응·35/39 계획 집합·문서 링크 검사 |
| [generation.txt](generation.txt) | 원본 재생성 결과 동일 검사 |
| [mcp.json](mcp.json) | 빌드한 독립 서버에 대한 공식 SDK 발견·호출·프로토콜 오류 |
| [codex.json](codex.json) | 실제 설치 Codex app-server의 인증 helper·발견. 모델 턴 호출과 구분 |
| [cli.json](cli.json) | 빌드한 CLI의 PTY 계좌 선택·YES 확인·철회·상태·정상 종료 |
| [build.txt](build.txt) | 실행 파일 형식·버전·OS framework 연결·체크섬 |

결과에는 Toss 비밀·접속 credential·원본 실제 계좌 식별자를 기록하지 않는다. 모의 helper 출력은 테스트 클라이언트가 파이프로만 받는다. 현재 실행 파일과 최종 SHA-256은 `bin/tossinvest-tools`, `bin/SHA256SUMS`에 있다.

## 수용 기준과 실제 증거

| 기준 | 검증 근거와 결과 |
|---|---|
| T-001·T-032 | `TestPinnedCoverageAndRefs`, scripts/check-specs.py: 원본 해시/크기/버전·내부 ref·41개 REST·10개 WS·도구 중복/문서 링크·대응 증거 경로 검사 |
| T-002·T-011·T-012 | `TestAllOfficialResponseExamples`의 조회 성공/오류 예제 281개, `TestDecimalAndTime`, `TestURLDateOffset`: 원본 3.1 생성/컴파일, oneOf/allOf/null·form·Decimal·offset/DST 보존. ETF englishName 불일치는 좁은 adapter로 기록 |
| T-003·T-006·T-031 | `TestAll34RESTInputsResponsesPermissionsErrors`: 34개 각각 필수/전체 path·query·옵션 직렬화, 공식 응답 typed/raw 처리, 400/401/403/404/429/500와 계좌 제한. `TestInputBoundaries`: 필수 값·enum·최대값·symbols·기간 조합. `TestTokenFormSingleflight401`: 미지 응답 enum/필드 보존 |
| T-004·T-008·T-009 | `TestAll34ServicePermissionScopeInputError`, `TestR1MCPDiscoveryCallsAndAuth`: 34개 각각 scope·account_alias·헤더 직접 입력·오류, Host·Origin·HTTP 토큰·TCP owner 라우트 미노출. CLI는 동일 서버 소켓만 사용 |
| T-005·T-022 | `TestTokenFormSingleflight401`: 동시 12개 401이 토큰 총 2회(최초+한 번 갱신)로 수렴. `TestActualRateGroupsAndDynamicHeaders`: 고정 원본 group, 동적 limit/reset/Retry-After·취소·이미 대기 중인 요청의 새 backoff 적용 |
| T-007·T-033~T-035 | `TestReadonlyFinalTransportGuard`: 6개 생성 쓰기 메서드 직접 호출·허위 라벨·외부 origin·비등록 경로·인코딩·redirect 차단, 거부 요청 전송 0건. core·MCP·CLI 미노출/직접 호출 거부. `TestBackendIsolationWithFakeSecretsNoKeychain`: toss/mock readonly 및 trading 거부·DB/credential 분리 |
| T-010·R-017 비밀 경계 | 실제 키 없음. native Keychain adapter 컴파일/OS framework 연결, fake SecretStore·mock 분리. MCP/CLI 결과의 accountSeq/accountNo/접속 credential 미노출을 독립 프로세스 client가 확인. 실제 Keychain UX는 별도 대기. T-017의 주문 승인 소비는 T1 범위 |
| T-024·T-027·T-030 | OPEN 전량/CLOSED 페이지 의미를 요청·원본 응답으로 보존. WS 복구가 holdings·OPEN/CLOSED 일반/조건 목록을 페이지 처리. `TestRecoveryOverlapCannotPublishOlderSnapshot`: 수신 버전이 바뀐 snapshot의 게시 거부/재조회. `TestWSReconnectHeartbeatStorageFailureAndRevocation`: 실제 로컬 WS 단절·DB 실패·재구독·겹침 복구와 gap 유지. store 테스트: 실제 백업/복원·backend 혼용·손상 거부 |
| T-025·T-026 | `TestWSAllMessagesVariantsAckAndDeclarationErrors`: KR/US 체결·호가, 모든 명세 주문 이벤트 및 미지 event, ACK echo·stale id·부분 거부·전체 오류의 기존 집합 유지·shutdown. 실제 로컬 WS heartbeat·재연결과 선언 합계/간격 제한. upstream의 계정 2연결 제한 자체는 당 서버가 연결 1개만 사용하며 live 확인 대기 |
| T-029·T-036~T-038 | R1/R2 registry·readiness와 최소 CLI exit code·schema/path/helper 테스트, `TestServiceDryRunReadonlyAndNoInstallation`의 readonly argv·plist escaping/재시작 제한. PTY 실제 CLI 선택·철회. 실제 launchd·잠긴 Keychain·공인 IP는 별도 대기 |
| T-039 | `TestEmptyAccountsAndRevocationInFlight`, `TestR1MCPDiscoveryCallsAndAuth`: 미선택 accounts는 upstream 없이 local_policy 빈 집합/선택 필요, 원본 번호/seq 비노출. UID 소켓 발견·grant·revision/별칭 원자 적용 |
| T-040 | `TestGrantExpiryConflictUnknownPrincipalAliasAndClient`, `TestDiscoveryDisappearanceAndAliasTombstones`, `TestPrivatePathsSymlinksAndLock`, `TestPeerUIDBoundary`: 60초 만료·revision 충돌·사라진 계좌·별칭 재할당/이름 변경·client 신원 변경·다른 UID 비교 거부. 실제 동일 UID 소켓 통과, TCP 변경 라우트/비TTY 설정 거부. 철회 중 응답·이벤트·상태 비노출, upstream 장애 중 clear 및 대기 중 새 전송 금지. 다른 실제 OS UID로 접속하는 특권 운영 검증은 미실행 |
| T-041 | `TestUnknownIDsAccountBoundNoFallback`: 앱 신규 주문/조건 ID unknown은 지정 계좌 단건 GET 1회, confirmed 타 계좌/conflict는 사전 거부. 404·오류는 관계를 만들거나 다른 계좌로 fallback하지 않음 |
| T-042 | `TestMockCredentialLifecycleSingleInstanceAndMode`, `TestBackendIsolationWithFakeSecretsNoKeychain`: 무인증·잘못된/이전 credential, UID/0600/0700·symlink·run_id·종료 정리·회전·backend DB/credential 분리, 실제 provider 호출 0건. CLI 프로세스 강제 종료 후 stale 파일/소켓의 접속 거부와 재시작 credential/run_id 회전도 검증. mock에 실제 네트워크 fallback 경로 없음 |
| T-043 | `TestActualHistoryCountBoundaryAndSevenDays`, `TestActual256MiBPayloadBoundary`: 실제 100,001건과 256 MiB+1바이트·7일 등호 경계/prefix/watermark. `TestCursorRetentionRevisionRestartTamper`, `TestCursorAfterDatabaseReopen`: MAC·다른 계좌/미래 위치·재시작/권한 revision·watermark 등호·만료·철회 우선. `TestEventHistoryPrefixAndJournalIsolation`: 시각 역행·계좌별/전체 payload·저널 보존 |
| T-044 | `TestQueueExactLimitsAndAge`: 1024건·8 MiB·1초 등호. `TestActualWSFrameLimitAndQueuedDispatchRevocation`: 실제 WS 1 MiB와 +1바이트 재연결. `TestSlowWriterQueueOverflowDoesNotBlockReader`: 포화 즉시 gap·미준비. 실제 DB 실패 후 복구·수신/REST 겹침·시세 coalescing, 재연결 뒤 관측 gap 유지 |

주문 승인·멱등 제출·정정/취소·노출 한도·pause 등 T-013~T-023·T-028의 전체 쓰기 수용은 이번 범위 밖이다. 조회/readonly 방어에 해당하는 일부 검증만 수행했다. 조건주문과 일반 주문의 실제 생성 기능을 검증했다고 표시하지 않는다.

## 실제 Codex 검증 방법과 한계

`scripts/check-codex.py`가 임시 app-dir와 별도 독립 mock 서버를 만든 뒤 설치된 `codex app-server --stdio`에 프로세스 한정 설정을 전달한다. `initialize` → `initialized` → `mcpServerStatus/list(serverName=tossinvest_test, detail=toolsAndAuthOnly)`를 사용한다. 이 과정에서 Codex 자신의 MCP client가 문자열 `http_headers_helper`를 실행하고 HTTP 서버의 35/39개 도구를 발견한다. 사용자 config.toml은 바꾸지 않는다.

초기 command/args 객체 helper 설정은 이 클라이언트에서 `expected a string`으로 거부됐고, 공식 문서와 설치된 클라이언트에 맞춰 문자열로 교정했다. 최종 성공 결과는 codex.json이다. SDK 프로토콜 테스트만 수행하고 Codex 연결 성공이라고 바꾸어 기록한 것이 아니다.

MCP 대표 호출은 cmd/mcp-check에서 공식 SDK를 이용해 별도로 확인했다. 현재 채팅의 지속 등록·모델 직접 호출·사용자 Keychain·실계좌는 확인하지 않았다. [사용자 등록 및 재연결 절차](../local-operation.md#codex-mcp-등록)를 따른다.
