# tossinvest-tools

토스증권 공식 Open API의 readonly REST·실시간 조회를 macOS의 독립 서버, MCP와 최소 CLI로 제공합니다. 공식 제품은 아닙니다.

**readonly R2 0.2.0을 구현했고, 실제 키 없이 고정 명세·로컬 HTTP/WS mock으로 검증했습니다.** 실제 Toss REST/WS adapter가 포함되어 있습니다. 주문 모듈은 후속 T1이며 `--mode=trading`은 mock/toss 모두 거부합니다.

| 구분 | 확인한 범위 |
|---|---|
| 구현·모의 검증 | REST 인증 + 조회 34개, MCP/CLI, Keychain adapter, 계좌 선택·권한, WS 전체·재연결·보존 |
| 독립 서버 MCP 검증 | 공식 Go MCP SDK v1.8.0으로 R1 35개/R2 39개 발견, 각각 조회 34개와 실시간 대표 호출 |
| 실제 Codex 클라이언트 | Codex 0.164.0-alpha.11.1 app-server에서 인증 helper와 35개/39개 도구 발견 |
| 사용자 연결 대기 | 이 채팅/데스크톱의 지속 등록·재연결 후 모델 직접 호출, 실제 Keychain UX·실계좌·출구 IP·절전·launchd 운영 |

최근 사용 준비·시크릿 재점검과 보완 내역은 [검토 기록](docs/verification/readiness-audit/README.md)에 있습니다. `./scripts/bootstrap-audit` 후 `make audit-secrets`로 인덱스·publish 후보·Git 객체/이력을 로컬 재검사할 수 있습니다.

전체 결과·명령·클라이언트 버전·수용 기준별 근거는 [검증 기록](docs/verification/README.md)에 있습니다. 모의 검증을 실계좌 검증으로 표시하지 않습니다.

## 빌드와 키 없는 실행

Go 1.27.3과 고정 의존성은 저장소 `.tools`에 준비됩니다. 빌드에는 macOS ARM64 Command Line Tools, 검증 스크립트에는 Python 3가 필요합니다. 실행에는 Go·Node·OpenAI API 키가 필요 없습니다.

```sh
./scripts/bootstrap
make verify
make build
./bin/tossinvest-tools serve --backend=mock
```

다른 터미널에서 계좌를 선택합니다. `1:primary` 같은 index:alias와 최종 `YES` 확인을 입력합니다.

```sh
./bin/tossinvest-tools setup accounts --backend=mock
./bin/tossinvest-tools doctor --backend=mock --json
```

실행 파일은 `bin/tossinvest-tools`, 체크섬은 `bin/SHA256SUMS`입니다. R2가 기본이며 `serve --milestone=R1`은 35개 도구로 실행합니다. `make check-generated`는 고정 원본에서 재생성 결과가 동일한지 검사합니다. `make mcp-check`, `make codex-check`, `make cli-check`로 독립 서버·실제 Codex 발견·TTY 계좌 선택을 재검증할 수 있습니다.

## CLI로 조회

실행 중인 서버에 다음 명령으로 조회합니다. 실제 연결에는 `--backend=toss`, 모의 연결에는 `--backend=mock`를 사용합니다.

```sh
./bin/tossinvest-tools accounts --backend=toss --json
./bin/tossinvest-tools holdings --account-alias=primary --backend=toss --json
./bin/tossinvest-tools holdings --account-alias=primary --symbol=005930 --backend=toss --json
./bin/tossinvest-tools prices --symbols=005930,AAPL --backend=toss --json
```

`primary`는 `setup accounts`에서 허용한 별칭으로 바꿉니다. 계좌 목록은 허용된 마스킹 계좌만 반환하며 미선택/철회한 계좌의 자산 조회는 거부합니다. CLI는 기존 MCP 접속 credential로 같은 서버의 `/mcp`를 호출하고 `data`·`meta`를 JSON으로 출력합니다. 서버가 없으면 exit 3이며 Toss에 직접 접속하지 않습니다. [CLI 조회 검증](docs/verification/cli-queries.md)에 모의 결과를 기록했습니다.

## Codex 연결

사용자가 Codex 설정에 다음을 등록하고 MCP 연결을 Restart합니다. 이 작업에서 사용자의 등록 설정은 변경하지 않았습니다.

```toml
[mcp_servers.tossinvest]
url = "http://127.0.0.1:17840/mcp"
http_headers_helper = "'/Users/bright/Desktop/work/tossinvest-tools/bin/tossinvest-tools' mcp-auth-header --backend=mock"
startup_timeout_sec = 20
tool_timeout_sec = 60
```

helper는 클라이언트가 직접 실행하며, 결과 credential을 채팅에 복사하지 않습니다. mock credential은 서버 재시작마다 회전합니다. 서버는 Codex의 수명과 독립적입니다. [공식 Codex MCP 문서](https://learn.chatgpt.com/docs/extend/mcp?surface=cli)와 [로컬 연결 안내](docs/local-operation.md#codex-mcp-등록)를 참고하세요.

실제 연결은 사용자가 로컬 `setup --backend=toss`에서 키를 숨김 입력한 뒤 `serve --backend=toss` → `setup accounts --backend=toss` 순서로 준비합니다. 그다음 MCP helper도 `--backend=toss`로 바꾸고 재연결합니다. 키·토큰을 채팅·CLI 인자·Git에 넣지 않습니다. 실제 키 등록·실계좌 조회·서비스 설치·주문·push·배포는 수행하지 않았습니다.

## 범위와 운영

서버는 `127.0.0.1:17840/mcp`에서 인증된 Streamable HTTP를 제공합니다. 허용 계좌는 빈 집합에서 시작하고 owner 소켓으로 선택한 별칭만 노출합니다. readonly는 서비스와 최종 broker transport에서도 쓰기를 차단합니다. mock는 별도 DB·실행별 credential을 사용하며 실제 Keychain이나 Toss 네트워크로 fallback하지 않습니다.

시세는 최신 스냅샷, 주문 이벤트는 7일·계좌당 100,000건·전체 payload 256 MiB로 보존합니다. 연결 누락과 보존 삭제·현재 REST 동기화를 구분하며 원장 완전성을 주장하지 않습니다. launchd 설치·중단·제거, offline 백업·복원 명령과 실제 사용자 설정 절차는 [로컬 운영](docs/local-operation.md)에 있습니다.

원본 REST 41개 중 인증 1개와 조회 34개, 별도 AsyncAPI 10개를 이번 릴리스로 검증했습니다. 쓰기 6개와 승인·주문 실행은 planned입니다. 전체 공식 API 완료와 readonly R2 완료를 구분합니다.

후속 작업은 [AGENTS.md](AGENTS.md), [인수인계](docs/handoff.md), [요구사항](docs/requirements.md), [API 대응 데이터](docs/api/operations.json)를 확인합니다. 설계·보안·원본·변경 절차의 전체 목록은 [문서 안내](docs/README.md)에 있습니다.
