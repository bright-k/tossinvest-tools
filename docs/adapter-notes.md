# 원본 호환 처리와 런타임 경계

2026-10-26의 고정 REST 1.2.24 / WS 1.2.2를 사용한다. 원본 JSON 두 개는 변경하지 않았다.

- oapi-codegen v2.8.0으로 OpenAPI 3.1 원본의 41개 작업과 모든 모델을 생성한다. `scripts/generate-adapter.py`는 생성된 함수 시그니처에서 34개 조회의 typed 요청/응답 dispatcher를 생성한다. 생성 파일은 직접 편집하지 않는다.
- 요청은 원본 schema의 타입·필수 값·enum·범위·패턴·날짜/offset을 검사한 뒤 생성 빌더로 직렬화한다. 서버 계좌 헤더는 `account_alias`로 해석하고 직접 입력을 거부한다. symbols 200개, 기간 순서, realtime 등락 랭킹, 국채 분봉 제약도 검사한다. 원본 optional 기본값은 upstream 기본값에 맡기며, 명시한 false/0와 생략을 혼동하지 않는다.
- 생성 타입과 별개로 `json.Decoder.UseNumber`를 사용한 JSON을 보존해 새 필드·미지 응답 enum·Decimal 문자열·null·원본 시각·페이지 정보를 반환한다. 응답 검증은 enum에 대해 개방적이며 필수 구조·타입·Decimal 형식은 검사한다. 원본 `oneOf`의 null 분기도 처리한다. 원본 시각 없는 응답의 `data_timestamp`, 판단할 수 없는 `stale`은 null이다. 캔들 종료 시각·미국 현지 날짜·DST를 임의 변환하거나 완성 봉으로 추정하지 않는다.
- 공식 `getStocks`의 ETF 예제는 `englishName`이 필수라고 선언된 타입에서 그 필드를 생략한다. 이 조합에만 응답 필수 필드 검사를 완화한다. 빈 문자열이나 추정값을 채우지 않는다. 원본·생성 코드는 그대로이며 `internal/contract/catalog.go`와 `TestAllOfficialResponseExamples`에 근거가 있다. 다른 필수 필드는 계속 검사한다.
- 오류는 OAuth와 업무 envelope를 구분한다. 원본 HTTP 상태·code·requestId·Retry-After·새 data hint를 유지하고, message는 로컬 설명을 사용한다. credential/authorization/accountSeq/accountNo 계열 hint 필드는 제거한다. upstream 오류를 빈 정상 데이터로 변환하지 않는다.
- 최종 HTTP transport는 caller의 operationId 대신 실제 origin·method·경로를 검사한다. 인증 POST와 고정 조회 GET만 허용한다. redirect·임의 origin·비등록 경로·모호한 인코딩·여섯 쓰기는 거부한다. 생성된 쓰기 메서드를 직접 호출하는 테스트도 전송 0건이다.
- 공식 공개 [연동 안내](https://openapi.tossinvest.com/openapi-docs/overview.md)의 Rate Limits Group과 `X-RateLimit-*` 의미를 2026-10-26 확인했다. 그룹은 고정 원본의 operation description에서 읽는다. ACCOUNT/STOCK_ALL은 1초 간격, MARKET_INFO/ORDER_INFO는 최소 334ms, 나머지 조회는 보수적 250ms를 기본으로 쓰며 동적 Limit·Remaining·상대 초 Reset·Retry-After로 더 제한한다. 대기 중인 호출도 새 backoff와 현재 계좌 권한을 재검사한다. 429/5xx는 구조화 오류로 반환하며 자동 재시도 폭주를 만들지 않는다. 401만 토큰 비교에 기초해 한 차례 갱신·조회 재시도한다. AUTH 역시 중앙 제한을 사용한다.
- macOS Keychain은 Security.framework에 직접 연결한다. 비밀을 `security -w SECRET` 같은 프로세스 인자에 넣지 않는다. Toss client와 MCP credential은 고정된 별도 service/account 항목이다. mock에서는 해당 API를 호출하지 않는다. Keychain 잠김·접근 거부 시 평문 fallback은 없다. 실제 Keychain 등록/잠금 UX는 사용자 opt-in 검증 대기다.
- R2는 한 WS 연결에서 full-replace 선언, id echo, ACK 부분 거부, 전체 오류 시 기존 집합 유지, PING/pong, 재연결을 처리한다. 거부 topic은 명시적 watchlist 갱신 전까지 제외한다. 시장 snapshot은 topic별 최신 값으로 합친다. 개인 이벤트는 별도 제한 큐와 SQLite writer를 사용한다.
- 재연결은 구독/수신을 먼저 열고 REST holdings·OPEN·CLOSED 일반/조건 목록을 겹쳐 조회한다. CLOSED 등 페이지를 소진하며 반복 cursor/상한 초과는 동기화 실패다. 수신 버전이 바뀐 복구 snapshot은 게시하지 않고 재조회한다. 원시 수신 이력은 중복·역순도 관측 순서대로 보존하며 upstream 시각을 상태 버전으로 오해하지 않는다. REST 복구 후에도 과거 gap은 유지한다.

실제 토스의 계좌 헤더가 단건 자원을 제한한다는 전제와 실계좌 응답·출구 IP·실시장 재접속은 검증하지 않았다. 이는 mock 계약 검증과 별도다. 주문 모듈은 후속 T1이며 현재 실행 파일의 toss/mock 모두 trading 시작을 거부한다.

## 사용 준비 재검토 보완

복구용 REST에도 일반 조회와 동일한 계좌 revision/전송 guard를 적용하고 snapshot 게시 시 재검사한다. 연결 상태만으로 데이터 freshness를 확정하지 않으며, 연결 중에는 stale=null, 단절 후에는 stale=true로 반환한다. CLI clear는 upstream 발견 없이 로컬 철회한다. 근거와 회귀 검증은 [준비 상태 재점검](verification/readiness-audit/README.md)에 있다.
