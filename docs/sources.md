# 출처와 확인 기준

확인 기준일은 2026년 10월 26일 Asia/Seoul입니다. API의 정확한 형태는 프로젝트가 고정한 원본을 기준으로 하고, 최신 문서와 차이가 생기면 명세 갱신 절차로 검토합니다. 구현 선택은 외부 문서의 사실과 프로젝트의 설계 판단을 구분합니다.

## 토스 공식 문서

| 출처 | 사용하는 범위 |
|---|---|
| [개발자 문서](https://developers.tossinvest.com/docs) | 사용자용 공식 가이드 진입점 |
| [LLM 문서 색인](https://developers.tossinvest.com/llms.txt) | 원본 명세·안내 경로 확인 |
| [OpenAPI JSON](https://openapi.tossinvest.com/openapi-docs/latest/openapi.json) | REST endpoint·요청·응답·오류·인증·제약 |
| [AsyncAPI JSON](https://openapi.tossinvest.com/openapi-docs/latest/asyncapi.json) | 실시간 채널·선언·메시지·연결 제약 |
| [연동 안내](https://openapi.tossinvest.com/openapi-docs/overview.md) | 시작 절차·동적 호출 한도·WS 연동 |
| [FAQ](https://openapi.tossinvest.com/openapi-docs/faq.md) | 멱등키·응답 유실·조회 범위·시세 의미·데이터 이용 조건 |

REST와 AsyncAPI의 로컬 원본·버전·해시는 [스냅샷](../specs/tossinvest/README.md)에 있습니다. 색인·FAQ·화면의 설명과 canonical 명세가 충돌하면 구체적인 충돌을 기록하고 확인합니다. 충돌을 임의로 병합하지 않습니다.

## 구현과 연결 문서

| 출처 | 사용하는 범위 |
|---|---|
| [Go 공식 릴리스](https://go.dev/doc/devel/release) | 지원 중인 도구체인과 보안 패치 확인 |
| [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk) | 공식 SDK·프로토콜 버전·transport·인증 API |
| [Rust MCP SDK](https://github.com/modelcontextprotocol/rust-sdk) | 언어 선택 대안 |
| [TypeScript MCP SDK](https://github.com/modelcontextprotocol/typescript-sdk) | 언어 선택 대안 |
| [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) | 타입·REST 클라이언트 생성 |
| [OpenAPI 3.1 지원 릴리스](https://github.com/oapi-codegen/oapi-codegen/releases/tag/v2.8.0) | 3.1 초기 지원과 검증 전제 |
| [coder/websocket](https://github.com/coder/websocket) | Go WS 연결 후보 |
| [modernc SQLite](https://pkg.go.dev/modernc.org/sqlite) | CGo 없는 SQLite 드라이버 후보 |
| [shopspring Decimal](https://github.com/shopspring/decimal) | 문자열·정확한 십진 계산 후보 |
| [Codex MCP 연결](https://learn.chatgpt.com/docs/extend/mcp?surface=cli) | 로컬 HTTP·STDIO·인증·설정과 header helper |
| [OpenAI MCP 서버 구현](https://developers.openai.com/plugins/build/mcp-server) | 도구 schema·권한·annotation과 서버 검증 경계 |
| [OpenAI MCP 인증](https://developers.openai.com/plugins/build/auth) | 원격 사용자 OAuth 등 후속 연결 설계 |

## macOS 운영

- [launchd 사용자 서비스](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html): 로그인 사용자에 속한 백그라운드 프로세스와 수명.
- [Keychain Services](https://developer.apple.com/documentation/security/keychain-services/): 비밀 저장과 접근 제어.
- [Keychain 사용자 확인](https://developer.apple.com/documentation/localauthentication/accessing-keychain-items-with-face-id-or-touch-id): 실주문 승인 방식의 후보를 검토할 때 사용할 OS 기능.

라이브러리 지원 설명은 이 토스 원본으로 생성·빌드·동작했다는 증거가 아닙니다. 실제 버전과 결과는 P0에서 고정하고 기록합니다. 비공식 토스 MCP 프로젝트의 코드나 보안 보증을 이 기준에 가져오지 않았습니다.
