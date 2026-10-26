# Go 단일 서버와 명세 기반 전체 지원

- 날짜: 2026년 10월 26일
- 상태: 구현 기준안. 실제 원본 생성·컴파일·디코딩과 클라이언트 연결을 P0에서 검증한다.
- 관련 요구사항: R-001~R-005, R-016, R-021~R-026, R-028, R-031~R-032

## 맥락

사용자는 `tossinvest-tools` 저장소에서 서버·MCP·CLI를 제공하고 로컬 PC에서 우선 운영하려고 합니다. 공식 API 전체 지원과 후속 변경 대응이 목표입니다. 일회성 도구보다 장기 실행의 토큰·소켓·주문 상태 관리가 중요합니다.

## 결정

Go의 지원 중인 안정 버전을 고정하여 단일 서버 실행 파일을 만듭니다. 서버는 `net/http` 위에서 공식 Go MCP SDK의 Streamable HTTP와 최소 운영 기능을 제공합니다. CLI의 업무 요청도 같은 서버를 거칩니다.

원본 OpenAPI로 REST client와 타입을 생성하고, 별도 service 계층에서 계좌 권한·주문 제약·승인과 복구를 처리합니다. AsyncAPI는 별도 WS 관리자의 구현·계약 검증 기준으로 사용합니다. SQLite에 로컬 상태를 저장하고 OS별 비밀 저장·서비스 관리는 adapter로 나눕니다.

Go MCP SDK·oapi-codegen·coder/websocket·modernc.org/sqlite·shopspring/decimal을 초기 후보로 합니다. 최신 버전을 무조건 적용하지 않고 P0에서 검증한 버전과 지원 프로토콜을 고정합니다. `go.mod`와 생성기 설정을 만든 시점에 실제 버전·결과를 이 ADR에 기록합니다.

## 대안

| 대안 | 장점 | 이번 기준에서의 비용 |
|---|---|---|
| TypeScript와 Node | MCP·스키마·웹 UI 개발이 편리 | 사용자 PC의 런타임·패키지·운영 경로 관리가 추가됨 |
| Rust와 rmcp | 강한 타입·소유권과 실행 파일 배포 | 전체 API 변경·업무 로직 구현의 복잡도가 증가함 |
| Python과 공식 SDK | 분석 라이브러리와 Decimal 활용이 편리 | 런타임·가상환경 운영이 필요하고 초기 목표가 분석 엔진은 아님 |
| Codex가 시작하는 STDIO 서버 | 초기 연결이 단순 | 여러 세션의 토스 토큰·소켓·주문 복구를 별도로 통합해야 함 |
| 서버·작업자·DB를 별도 서비스로 분리 | 독립 확장 가능 | 개인용 로컬 운영에 배포·분산 상태 관리 비용이 큼 |

## 결과와 검증 조건

실행 파일 중심의 설치와 운영이 가능하고 핵심 서비스는 OS에 덜 종속됩니다. 대신 Go의 생성 타입과 schema 표현이 원본의 모든 경우를 처리하는지 확인해야 합니다. `oapi-codegen`의 OpenAPI 3.1 지원은 실제 토스 원본 호환성 검증을 대체하지 않습니다.

P0에서 form 인증·oneOf·allOf·null union·Decimal·예제·오류·프로토콜 협상을 검증합니다. 보정은 원본 바깥에 기록합니다. 호환성 실패를 해결하는 데 과도한 수작업이 필요하거나 UI·분석의 비중이 제품 중심으로 바뀌면 언어·생성기 선택을 재검토합니다.

## 확인 근거

- [공식 Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk)
- [공식 Rust MCP SDK](https://github.com/modelcontextprotocol/rust-sdk)
- [oapi-codegen OpenAPI 3.1 지원](https://github.com/oapi-codegen/oapi-codegen/releases/tag/v2.8.0)
- [프로젝트에서 고정한 토스 명세](../../specs/tossinvest/README.md)

## P0 검증 결과 — 2026-10-26

Go 1.27.3 darwin/arm64, 공식 MCP SDK v1.8.0, oapi-codegen v2.8.0, runtime v1.6.0, coder/websocket v1.8.14, modernc SQLite v1.39.1, shopspring decimal v1.4.0을 고정했다. 정확한 전이 버전과 합계는 go.mod/go.sum이다. Go archive SHA-256은 scripts/bootstrap에 고정하며 .tools 외의 시스템 도구체인을 설치하지 않았다.

원본 3.1에서 41개 작업 생성·컴파일, 34개 조회의 성공/오류 예제 281개, form 인증·allOf/oneOf/null·Decimal·추가 필드·미지 enum을 검증했다. 재생성 결과가 동일했다. 생성 함수에 연결하는 dispatcher도 생성 스크립트로 재현한다. 원본 예제와 필수 선언의 ETF englishName 불일치는 원본 밖의 좁은 응답 adapter로 기록했다. [adapter 근거](../adapter-notes.md)를 따른다.

공식 SDK 클라이언트로 빌드한 독립 HTTP 서버의 35/39개 도구 발견과 조회/실시간 호출을 검증했다. 설치된 Codex 0.164.0-alpha.11.1은 별도 app-server에서 문자열 http_headers_helper 인증과 35/39개 발견을 확인했다. 이 채팅의 지속 등록·모델 직접 호출은 별도 재연결 확인 대기다. [실행 근거](../verification/README.md)에 프로토콜 검증과 Codex 검증을 분리한다.

구현은 core·runtime·broker·store·platform·cli로 나눴다. 배포 실행 파일은 OS의 Security/CoreFoundation에 직접 연결하므로 실제 Keychain 저장에 외부 security CLI나 언어 런타임이 필요하지 않다. Keychain OS UX와 launchd 설치는 기본 테스트에 섞지 않았다.

## CLI 업무 조회 확장 — 2026-10-26

계좌 목록·자산·현재가 CLI는 기존 Streamable HTTP MCP의 3개 고정 도구를 공식 Go SDK로 호출한다. 새 업무 HTTP 라우트나 CLI용 broker를 만들지 않는다. runtime.ConnectionCredential로 같은 최소 권한 주체를 인증하고 계좌 정책은 서버에 둔다. loopback 연결은 proxy와 redirect를 사용하지 않으며 credential을 인자/일반 출력에 넣지 않는다. SDK의 any 디코딩에 따른 정밀도 손실을 피하기 위해 서버가 structuredContent와 동일하게 내보내는 JSON text 복사본을 UseNumber로 읽는다. 새 의존성/DB migration/MCP 도구 변경은 없다. 근거는 [CLI 조회 검증](../verification/cli-queries.md)이다.
