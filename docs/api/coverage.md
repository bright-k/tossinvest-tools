# API 지원 계약

공식 REST와 별도 WebSocket 명세의 전체 기능을 지원 대상으로 삼습니다. 현재 원본은 REST OpenAPI 3.1.0·API 1.2.24와 AsyncAPI 3.0.0·API 1.2.2입니다. REST는 38개 경로의 41개 작업이며, 내부 인증 1개·조회 34개·승인 후 실행하는 쓰기 6개로 구분합니다. 인증 1개·조회 34개·WS 10개와 R1/R2 운영 도구 5개는 고정 명세/모의 계약 `verified`입니다. 쓰기 6개와 T1 도구 2개는 `planned`이며 실행 파일은 trading을 거부합니다. 실계좌 `live-verified` 항목은 없습니다. [검증 근거](../verification/README.md)를 따릅니다.

작업별 대응의 기준 데이터는 [operations.json](operations.json)입니다. 원본과 해시는 [명세 스냅샷](../../specs/tossinvest/README.md), 요청·응답의 세부 제약은 [REST 원본](../../specs/tossinvest/openapi.json)과 [AsyncAPI 원본](../../specs/tossinvest/asyncapi.json)을 따릅니다.

## 노출 규칙

- 조회 도구는 원본의 유효한 필터·기간·페이지·시장 등 기능을 지원합니다. 도구의 계좌 입력은 서버에서 허용된 별칭으로 해석합니다.
- 인증은 내부 기능입니다. 토큰 발급·토큰 반환을 AI 도구로 노출하지 않습니다.
- 쓰기 행의 도구는 초안만 준비합니다. 실제 전송은 공통 `tossinvest_execute_approved_action`이 고정 초안과 사용자 승인 증거를 검증한 후 수행합니다.
- clientOrderId, 계좌·인증 헤더, 고액 확인 값은 서버가 관리합니다. 기능을 없애지 않으면서 권한을 제한합니다.
- readonly에서는 6개 준비 도구와 공통 실행 도구를 등록하지 않습니다. CLI와 운영 API도 같은 기능을 노출하지 않고, 직접·기존 정보 기반 호출은 서비스와 전송 경계에서 다시 차단합니다. trading을 명시 시작했을 때만 선택적으로 등록합니다.
- 모든 upstream 입력을 무검증 JSON으로 그대로 받는 범용 HTTP 도구는 제공하지 않습니다. 각 endpoint의 타입과 조합 검증을 유지합니다.

### 조회용 로컬 변환 계약

`getAccounts`의 MCP 결과는 허용 계좌의 별칭·유형·마스킹 번호만 반환합니다. 초기 허용 집합은 비어 있고 `setup accounts`에서 선택하며 원본 accountNo·accountSeq는 모델에 반환하지 않습니다. 다른 계좌 업무 입력에는 명시적인 account_alias가 필요합니다. [계좌 선택과 scope](../interfaces.md#계좌-선택과-접속-권한)를 따릅니다.

`getOrder`·`getConditionalOrder`의 unknown ID는 요청한 허용 계좌의 단건 GET으로만 확인하고, 확인된 타 계좌 관계는 전송 전에 거부합니다. 다른 계좌로 fallback하거나 404로 소유 관계를 추정하지 않습니다. 자세한 전제·오류·확인 관계는 [ID 계약](../interfaces.md#주문-id와-계좌-소유-관계)을 따릅니다.

`tossinvest_list_order_events`는 한 계좌 별칭, 불투명 cursor, limit 100(최대 1000)을 사용합니다. 보존 삭제·재시작·권한 revision 변경은 CURSOR_EXPIRED로 드러내고, REST 동기화가 누락 이벤트를 복원한 것으로 표시하지 않습니다. [페이지 계약](../interfaces.md#주문-이벤트-페이지와-커서)과 [수신·보존 한도](../architecture.md#이벤트-보존과-수신-큐)를 따릅니다.

대응 데이터의 선택 필드 `contract_reference`는 이 로컬 계약의 저장소 상대 경로와 anchor입니다. 원본 upstream schema의 변경이나 구현 증거가 아니며 런타임에 외부 문서를 읽어 권한을 바꾸는 설정도 아닙니다. 기존 schema_version 2에 추가하는 설명용 선택 필드이며 구현 증거는 별도의 status/verification_evidence에 기록합니다.

## 프로필별 범위

[operations.json](operations.json)의 `available_modes`와 `broker_effect`가 등록·전송 분류의 기준입니다. 인증·조회는 readonly/trading, 6개 주문 변경은 trading 전용입니다. 추가 운영 도구도 `additional_tools`에 추적합니다. R1은 REST 조회 34개와 서버 상태, R2는 실시간 4개 도구를 추가한 readonly를 우선 제공합니다. T1은 선택적 쓰기 전체이며 구현 전 상태는 planned로 유지합니다. 구체적인 등록 규칙은 [실행 프로필](../runtime-profiles.md)을 따릅니다.

## CLI 조회 확장

계좌 목록(`accounts`), 보유 자산(`holdings --account-alias=... [--symbol=...]`), 현재가(`prices --symbols=...`)를 같은 서버의 MCP로 호출한다. 대응 데이터의 선택 필드 `cli_command`에 노출 명령을 기록한다. 전체 34개 조회가 CLI에 제공된다는 뜻은 아니며 도구 이름/수/권한은 유지한다. [CLI 조회 검증](../verification/cli-queries.md)에 근거가 있다.

## REST 대응 목록

표의 도구 이름은 구현 기준안입니다. 이름을 바꿀 때 대응 데이터·테스트·문서를 함께 갱신합니다. 모든 필드의 input/output 스키마는 생성된 원본 타입과 이 계약을 연결해 P1 이후 구체화합니다.

| 작업 ID | 메서드와 경로 | MCP 또는 내부 기능 |
|---|---|---|
| `issueOAuth2Token` | `POST /oauth2/token` | `서버 내부 토큰 관리` |
| `getOrderbook` | `GET /api/v1/orderbook` | `tossinvest_get_orderbook` |
| `getPrices` | `GET /api/v1/prices` | `tossinvest_get_prices` |
| `getTrades` | `GET /api/v1/trades` | `tossinvest_get_trades` |
| `getPriceLimit` | `GET /api/v1/price-limits` | `tossinvest_get_price_limits` |
| `getCandles` | `GET /api/v1/candles` | `tossinvest_get_candles` |
| `getStocks` | `GET /api/v1/stocks` | `tossinvest_get_stocks` |
| `listStocks` | `GET /api/v1/stocks/all` | `tossinvest_list_stocks` |
| `getStockWarnings` | `GET /api/v1/stocks/{symbol}/warnings` | `tossinvest_get_stock_warnings` |
| `getStockInvestorTrading` | `GET /api/v1/stocks/{symbol}/investor-trading` | `tossinvest_get_stock_investor_trading` |
| `getStockProgramTrades` | `GET /api/v1/stocks/{symbol}/program-trades` | `tossinvest_get_stock_program_trades` |
| `getStockShortSelling` | `GET /api/v1/stocks/{symbol}/short-selling` | `tossinvest_get_stock_short_selling` |
| `getStockCreditTrades` | `GET /api/v1/stocks/{symbol}/credit-trades` | `tossinvest_get_stock_credit_trades` |
| `getStockSecuritiesLending` | `GET /api/v1/stocks/{symbol}/securities-lending` | `tossinvest_get_stock_securities_lending` |
| `getExchangeRate` | `GET /api/v1/exchange-rate` | `tossinvest_get_exchange_rate` |
| `getKrMarketCalendar` | `GET /api/v1/market-calendar/KR` | `tossinvest_get_kr_market_calendar` |
| `getUsMarketCalendar` | `GET /api/v1/market-calendar/US` | `tossinvest_get_us_market_calendar` |
| `getRankings` | `GET /api/v1/rankings` | `tossinvest_get_rankings` |
| `getMarketIndicatorPrices` | `GET /api/v1/market-indicators/prices` | `tossinvest_get_market_indicator_prices` |
| `getMarketIndicatorCandles` | `GET /api/v1/market-indicators/{symbol}/candles` | `tossinvest_get_market_indicator_candles` |
| `getMarketIndicatorInvestorTrading` | `GET /api/v1/market-indicators/{symbol}/investor-trading` | `tossinvest_get_market_indicator_investor_trading` |
| `listSectors` | `GET /api/v1/sectors` | `tossinvest_list_sectors` |
| `getSectorRankings` | `GET /api/v1/sectors/rankings` | `tossinvest_get_sector_rankings` |
| `getSector` | `GET /api/v1/sectors/{sectorId}` | `tossinvest_get_sector` |
| `getSectorStocks` | `GET /api/v1/sectors/{sectorId}/stocks` | `tossinvest_get_sector_stocks` |
| `getSectorEtfs` | `GET /api/v1/sectors/{sectorId}/etfs` | `tossinvest_get_sector_etfs` |
| `getAccounts` | `GET /api/v1/accounts` | `tossinvest_get_accounts` |
| `getHoldings` | `GET /api/v1/holdings` | `tossinvest_get_holdings` |
| `getOrders` | `GET /api/v1/orders` | `tossinvest_list_orders` |
| `createOrder` | `POST /api/v1/orders` | `tossinvest_prepare_order → 승인 실행` |
| `getOrder` | `GET /api/v1/orders/{orderId}` | `tossinvest_get_order` |
| `modifyOrder` | `POST /api/v1/orders/{orderId}/modify` | `tossinvest_prepare_order_modification → 승인 실행` |
| `cancelOrder` | `POST /api/v1/orders/{orderId}/cancel` | `tossinvest_prepare_order_cancellation → 승인 실행` |
| `createConditionalOrder` | `POST /api/v1/conditional-orders` | `tossinvest_prepare_conditional_order → 승인 실행` |
| `getConditionalOrders` | `GET /api/v1/conditional-orders` | `tossinvest_list_conditional_orders` |
| `getConditionalOrder` | `GET /api/v1/conditional-orders/{conditionalOrderId}` | `tossinvest_get_conditional_order` |
| `cancelConditionalOrder` | `DELETE /api/v1/conditional-orders/{conditionalOrderId}` | `tossinvest_prepare_conditional_order_cancellation → 승인 실행` |
| `modifyConditionalOrder` | `POST /api/v1/conditional-orders/{conditionalOrderId}/modify` | `tossinvest_prepare_conditional_order_modification → 승인 실행` |
| `getBuyingPower` | `GET /api/v1/buying-power` | `tossinvest_get_buying_power` |
| `getSellableQuantity` | `GET /api/v1/sellable-quantity` | `tossinvest_get_sellable_quantity` |
| `getCommissions` | `GET /api/v1/commissions` | `tossinvest_get_commissions` |

## 실시간 기능

| 기능 | 지원 범위 | 수용 기준 |
|---|---|---|
| 체결 | `trade:kr`, `trade:us` | 시장별 종목 검증, 가격·수량·시각·통화 보존 |
| 호가 | `orderbook:kr`, `orderbook:us` | REST와 같은 전체 스냅샷 처리, 증분으로 오해하지 않음 |
| 주문 | `personal:order` | 허용 accountSeq를 서버가 해석, 모든 주문 이벤트와 상태 처리 |
| 선언 | 단일 배열 full-replace | 기존 구독을 포함한 전체 집합 전송, 빈 집합·id echo·부분 거부 처리 |
| ACK | `subscriptions` | subscribed·rejected별 상태 반영, 거부 항목 무한 재선언 방지 |
| 오류 | `error` | 선언 전체 실패 시 기존 구독 유지, shutdown 후 재연결 |
| 유지 | 텍스트 `PING`, JSON `pong` | 60초 권장 주기, 서버 데이터 수신만으로 idle이 갱신된다고 가정하지 않음 |
| 복구 | 재연결·재선언·REST 동기화 | 누락 구간 표시, 계좌 준비 전 신규 쓰기 제한 |

AsyncAPI의 개별 작업도 다음과 같이 추적합니다.

| 작업 ID | 방향 | 채널 | 처리 |
|---|---|---|---|
| `subscribeTrade` | `send` | `realtime-trade` | 서버 WS 관리자 |
| `receiveTrade` | `receive` | `realtime-trade` | 서버 WS 관리자 |
| `subscribeOrderbook` | `send` | `realtime-orderbook` | 서버 WS 관리자 |
| `receiveOrderbook` | `receive` | `realtime-orderbook` | 서버 WS 관리자 |
| `subscribeOrder` | `send` | `realtime-order` | 서버 WS 관리자 |
| `receiveOrder` | `receive` | `realtime-order` | 서버 WS 관리자 |
| `receiveSubscriptionsAck` | `receive` | `connection` | 서버 WS 관리자 |
| `receiveError` | `receive` | `connection` | 서버 WS 관리자 |
| `sendPing` | `send` | `connection` | 서버 WS 관리자 |
| `receivePong` | `receive` | `connection` | 서버 WS 관리자 |

MCP에는 `tossinvest_set_subscriptions`, `tossinvest_get_stream_status`, `tossinvest_get_stream_snapshot`, `tossinvest_list_order_events`를 추가하는 기준안을 사용합니다. 구독 변경은 자금 쓰기와 다르지만, 주체·계좌·구독 한도·현재 revision을 확인합니다. 사용자 watchlist와 서버 필수 주문 구독을 병합하여 토스에 전체 집합을 선언합니다. watchlist 갱신으로 필수 주문 감시를 실수로 해제하지 않습니다.

초기 WS 기본값은 명세의 계정당 연결 2개·연결당 구독 100개·선언 초당 5회·클라이언트 송신 idle 180초를 고려합니다. 서버는 보통 한 연결로 통합하며, 변경된 한도는 명세 갱신 절차로 반영합니다. 체결·호가는 수신이 밀리면 유실될 수 있고 유실 sequence가 없습니다. 주문 이벤트의 무손실은 연결 세션 내부이며, 수신이 2초 이상 막히면 종료될 수 있습니다.

## 반드시 보존할 API 의미

| 영역 | 구현 기준 |
|---|---|
| 토큰 | client당 유효 토큰 1개, 재발급 시 기존 무효화, refresh token 없음 |
| 계좌 | accounts에서 accountSeq 해석, account 헤더는 필요한 작업에만 서버가 주입 |
| 조회 범위 | 지원하지 않는 호가 유형의 주문은 REST에서 빠질 수 있음. 빈 목록을 계좌 전체 주문 없음으로 확대하지 않음 |
| 주문 목록 | OPEN 전량과 CLOSED cursor·limit의 차이, 생성일 기준 필터와 부분 체결 종료 처리 |
| 일반 주문 | quantity와 orderAmount 중 하나, 시장·BUY/SELL·LIMIT/MARKET·TIF별 조합과 소수점 제한 |
| 정정 | KR 수량 필수와 US 수량 변경 불가 등 시장별 차이. 원본에 정의된 ID 관계 보존 |
| 조건주문 | SINGLE/OCO/OTO와 first·second·가격·방향·세션·유효일 제약. 수정 후 새 ID 사용 |
| 재시도 | 일반·조건 생성의 10분 멱등키와 동일 내용 규칙. 정정·취소의 동작을 생성과 혼동하지 않음 |
| 자금 | 미국 자동환전 미지원, 매수 가능 금액과 보유 평가액 구분 |
| 가격 | Decimal 문자열·호가 단위·상하한·시장 통화 보존 |
| 캔들 | 1분봉 종료 시각, 일봉 현지 거래일·KST 표현·DST, 완성 전 봉·조정주가 조건 |
| 미국 시세 | 일부 거래소 데이터이며 NBBO로 간주하지 않음. 분봉의 특수 체결·늦은 체결 가능성 반영 |
| 국내 시세 | KRX·NXT 통합 데이터와 SOR 주문. 통합 교차 호가를 자동 데이터 오류로 단정하지 않음 |
| 입력·응답 | 요청 enum은 제한하고 응답의 신규 필드·미지 enum은 보존. 알 수 없는 주문 상태는 보류 |
| 오류 | OAuth 오류와 업무 오류 envelope 구분, requestId·HTTP 상태·Retry-After 처리 |

표는 중요 의미를 찾는 안내이며 모든 제약을 대신하지 않습니다. 상세 제약은 고정 명세와 [공식 FAQ](https://openapi.tossinvest.com/openapi-docs/faq.md)를 확인합니다.

## 지원 상태 갱신

작업은 코드 존재 후 `implemented`, 필요한 계약·실패·권한·복구 검증 뒤 `verified`로 바꿉니다. `verification_evidence`에는 실제 존재하는 테스트·결과 문서를 저장소 상대 경로로 기록합니다. 실계좌 검증은 범위·날짜·사용자 허용을 별도 기록한 `live-verified`만 사용합니다. 생성 클라이언트가 컴파일된 사실만으로 전체 행을 verified로 바꾸지 않습니다.

새 명세에서 메서드·경로·operationId·파라미터·타입·enum·오류·한도·실시간 메시지를 비교합니다. 순서나 총 개수가 같더라도 의미 변경을 검토합니다. 삭제된 작업은 대응 목록에서 조용히 제거하지 않고 호환성과 마이그레이션을 기록합니다.
