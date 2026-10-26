# 실행 프로필과 도구 노출

서버의 기본 프로필은 `readonly`입니다. 주문 기능을 포함하려면 소유자가 프로세스 시작 시 `--mode=trading`을 명시합니다. 초기 사용과 첫 배포의 우선순위는 조회 전용이며, 실주문 기능의 구현·승인 화면·분리 실행을 기다리지 않고 조회 서버를 완성합니다.

이 문서는 실행 옵션·기능 등록·실제 전송 차단의 기준입니다. 애플리케이션 명령은 아직 구현되지 않았습니다. [ADR 0003](decisions/0003-readonly-first.md)에 사용자 요구와 선택 이유를 기록했습니다.

## 현재 readonly R2 릴리스

0.2.0은 readonly만 구현한다. `--mode=trading`은 toss/mock 모두 exit 2 / CAPABILITY_UNAVAILABLE로 시작을 거부한다. 아래 trading 동작은 후속 T1의 계약이다. 현재 R1 35개/R2 39개 도구의 발견·호출과 최종 전송 차단은 [검증 기록](verification/README.md)에 있다.

## 프로필과 연결 대상

`mode`는 제공할 업무 권한, `backend`는 접속할 데이터 소스입니다. 모의 데이터와 거래 권한을 같은 enum으로 섞지 않습니다.

```text
tossinvest-tools serve                              # toss + readonly
tossinvest-tools serve --mode=readonly              # 명시적 조회 전용
tossinvest-tools serve --mode=trading               # 선택적 주문 기능 포함
tossinvest-tools serve --backend=mock               # 모의 조회 전용
tossinvest-tools serve --backend=mock --mode=trading # 모의 승인·주문 검증
```

| backend | mode | 조회 | 주문 도구 등록 | 실제 토스 주문 |
|---|---|---|---|---|
| `toss` | `readonly` | 실제 조회 | 없음 | 불가능 |
| `toss` | `trading` | 실제 조회 | 구현·정책·승인 준비 검증 후 포함 | 개별 승인과 실행 검증 후 가능 |
| `mock` | `readonly` | 모의 조회 | 없음 | 불가능 |
| `mock` | `trading` | 모의 조회 | 모의 주문 검증을 위해 포함 | 불가능 |

`--mode` 생략은 항상 `readonly`입니다. 모드는 저장된 설정·환경변수·이전 세션·키 존재로 승격하지 않습니다. 일반 설정 JSON에는 `mode`와 `trading.enabled`를 두지 않습니다. `--backend`는 명시 옵션 → 비밀 없는 설정 → `toss` 기본값 순서로 결정합니다. `mock`에는 실제 키를 읽거나 토스 네트워크에 접속하는 fallback이 없습니다.

잘못된 mode/backend 값은 설정 오류로 종료합니다. 주문 구현이나 필요한 검증이 없는 실행 파일에서 `--mode=trading`을 요청하면 `CAPABILITY_UNAVAILABLE`로 종료하고 readonly로 조용히 바꾸지 않습니다. `toss + trading`에서 승인 검증·정책·실행 격리 조건이 불충분하면 `TRADING_NOT_READY`로 종료합니다. mock에서는 실제 키·실계좌 정책 대신 명시적인 모의 주체·모의 승인·모의 정책을 사용합니다.

mock에서도 MCP·상태 HTTP는 인증을 요구합니다. 실제 SecretStore 대신 실행별 mock credential provider와 분리 DB를 사용하고, 주체의 계좌 허용은 `setup accounts --backend=mock`에서 모의 계좌를 선택해야 생깁니다. 실제/모의 helper와 credential 경로는 섞지 않습니다. [mock 접속 계약](interfaces.md#mock-접속-credential)을 따르며 backend를 바꿔 시작해도 다른 backend의 허용 계좌를 상속하지 않습니다.

## 시작 시 확정하는 상한

프로필은 프로세스 생존 기간 동안의 최대 권한입니다. 실제 권한은 프로필 ∩ 접속 주체의 scope ∩ 계좌 정책 ∩ 준비 상태 ∩ 승인으로 결정합니다. trading 시작은 주문 한 건의 승인이 아닙니다. trading 서버에도 조회 전용 접속 토큰을 사용할 수 있습니다.

MCP·업무 CLI·운영 API에서 readonly를 trading으로 바꾸는 명령은 제공하지 않습니다. 설정 파일을 실행 중 수정해도 프로필이 바뀌지 않습니다. 변경하려면 소유자가 서버를 재시작하고 MCP를 재연결해야 합니다. 프로필이 고정되어도 pause·연결 단절·정책 변경·토큰 scope 축소는 권한을 더 제한할 수 있습니다.

## 기능 등록과 실행 차단

| 기능 | readonly | trading |
|---|---|---|
| REST 조회 34개 | 등록 | 등록 |
| 서버 상태 조회 | 등록 | 등록 |
| 체결·호가·주문 이벤트 조회 | 해당 실시간 구현 단계에서 등록 | 동일 |
| WS 구독 관리 | 허용 계좌·구독 정책 안에서 등록 | 동일 |
| 일반·조건주문 준비 도구 6개 | 등록하지 않음 | 승인 서비스 준비 후 등록 |
| `tossinvest_execute_approved_action` | 등록하지 않음 | 검증된 승인 작업 실행 |
| 신규 쓰기 pause 도구 | 등록하지 않음 | 범위 내 등록 가능 |
| 사용자 승인 발급·모드 전환·한도 상향 | AI 도구로 제공하지 않음 | AI 도구로 제공하지 않음 |

readonly에서 주문 준비·실행 도구는 `tools/list`에 없고, CLI 업무 도움말·사용 가능한 명령 목록과 운영 API 라우트에도 노출하지 않습니다. 정정·취소·조건주문 쓰기도 포함합니다. 주문·조건주문의 **내역 조회**와 주문 이벤트 구독은 계속 가능합니다.

목록에서 숨기는 것과 별개로 공통 서비스와 broker 전송 직전에도 readonly 금지 검사를 적용합니다. 이름을 알고 직접 호출하거나 오래된 MCP 도구 정보·DB·다른 인터페이스를 사용해도 실제 쓰기를 수행할 수 없어야 합니다. 등록되지 않은 도구는 프로토콜의 미등록 도구 오류, 숨겨진 로컬 CLI 기능은 사용 오류, 공통 서비스에서 막힌 호출은 `READONLY_VIOLATION`으로 구분합니다. 어떤 경우도 토스 쓰기 요청을 전송하지 않습니다.

readonly는 토스 계좌에 자금·주문 변경을 일으키지 않는다는 뜻입니다. SQLite 캐시·이벤트 기록, 토큰 발급, WebSocket 구독은 가능합니다. 토큰 발급도 POST이므로 HTTP 메서드 전체를 막는 방식을 사용하지 않습니다. `operations.json`의 `broker_effect`로 인증·조회·주문 변경을 분류하고, 등록되지 않은 새 작업은 기본 거부합니다.

broker 최종 전송 검사는 실제 method·허용 origin·매칭한 경로로 분류합니다. 호출자가 붙인 operationId나 read-only 라벨만 신뢰하지 않습니다. readonly에서 POST는 내부 토큰 발급 경로만 허용하며, 알려진 조회 GET·허용 WS 연결 이외의 요청은 기본 거부합니다. 인증 헤더를 임의 URL이나 다른 origin의 redirect로 전달하지 않습니다. 이 방어는 생성된 쓰기 메서드나 공통 서비스를 직접 호출해도 적용합니다.

`readOnlyHint`는 개별 도구 동작에 따라 표시합니다. WS 구독 집합을 바꾸는 도구는 readonly 프로필에서도 `readOnlyHint=false`입니다. 서버 프로필과 MCP annotation은 서로 대체하지 않습니다.

## 재시작과 미확정 주문

과거 trading 실행의 미사용 승인·전송 예약·미확정 요청이 DB에 있어도 readonly 시작에서는 실제 전송·재전송·정정·취소를 하지 않습니다. 조회만으로 결과를 확인하고 상태와 예약을 보수적으로 보존합니다. 10분 멱등 유효시간 안의 재시도라도 readonly에서는 금지됩니다.

이전 시작에서의 trading 플래그·승인·pause 해제 상태가 새 readonly 실행을 승격시키지 않습니다. 이미 토스에 접수된 주문이나 조건주문은 그대로 존속할 수 있으며, readonly가 외부에서 발생하는 체결을 막는 것은 아닙니다.

## launchd 계약

```text
tossinvest-tools service install                  # readonly를 명시한 argv로 등록
tossinvest-tools service install --mode=readonly
tossinvest-tools service install --mode=trading   # 소유자의 명시적 선택과 준비 검증 필요
```

서비스 설치는 선택한 모드를 실제 `serve --mode=...` argv로 기록합니다. 기본 설치가 기존 trading 설정을 암묵적으로 복사하지 않습니다. 기존 등록과 모드가 다르면 명시적 갱신 절차를 사용하며, 실행 중 서버 모드는 재시작 전 바뀌지 않습니다. 운영체제의 자동 재시작은 설치 때 명시한 argv를 재사용합니다. 사용자가 readonly로 다시 설치하면 trading 플래그를 제거합니다.

`service status`, `doctor`, 서버 상태 응답에는 실행 중 mode/backend와 등록된 서비스의 mode/backend를 구분해 표시합니다. config 파일의 값이나 서비스 등록 사실만으로 실제 프로세스 모드를 추측하지 않습니다.

## 상태와 준비 여부

인증된 상태 응답과 `tossinvest_get_server_status`는 mode, backend, 빌드의 구현 capability, 활성 capability, 조회 준비, 실시간 연결, 계좌별 동기화 상태와 제한 사유를 제공합니다. 키·토큰·전체 계좌 번호는 포함하지 않습니다.

- `query_ready`: 실제 또는 모의 조회를 수행할 기본 조건이 준비됐습니다. 모든 개별 query 성공을 보장하지 않습니다.
- `streams_ready`: 요청한 실시간 기능과 연결이 준비됐습니다. REST 조회 준비와 별개입니다.
- `trading_ready`: trading 프로필·scope·정책·승인 서비스·동기화 등 실행 조건이 준비됐습니다. 개별 주문 승인을 의미하지 않습니다.

readonly는 `trading_ready=false`가 정상입니다. 승인 서비스 미구현이나 실시간 기능을 아직 배포하지 않은 R1 릴리스 때문에 조회 서버 전체를 미준비로 만들지 않습니다. 토스 키가 없을 때 임의로 mock로 바꾸지 않고 `query_ready=false`와 설정 필요 상태를 표시합니다. live 토큰 갱신·Keychain·IP 문제도 같은 원칙으로 진단합니다.

허용 계좌 미선택은 계좌별 `ACCOUNT_SELECTION_REQUIRED` 제한입니다. 인증·시장 조회가 준비됐으면 query_ready=true일 수 있으나 계좌별 조회는 허용하지 않습니다. 주문 이벤트의 큐 포화·저장 실패는 관련 streams_ready와 동기화 상태에 반영하며 독립적인 REST 조회를 일괄 중단하지 않습니다. 권한 선택과 이벤트 보존은 각각 [접속 계약](interfaces.md)과 [보존 계약](architecture.md#이벤트-보존과-수신-큐)을 따릅니다.

## 첫 배포와 전체 지원

| 목표 | 필요한 단계 | 제공 범위 |
|---|---|---|
| R1 조회 전용 사용 가능 | P0~P2 | REST 조회 34개, 설정·Keychain·접속 인증, MCP, 최소 CLI와 진단 |
| R2 조회와 실시간 전체 | P3과 조회용 로컬 운영 | R1 + 전체 WS, 누락 표시·재동기화·서비스 운영 |
| T1 선택적 주문 기능 전체 | P4와 P6 전체 검증 | R2 + 6개 쓰기와 사용자 승인·안전한 복구 |

R1·R2는 우선 배포 가능한 명확한 결과입니다. 주문 구현을 생략한 채 프로젝트의 공식 API 전체 지원을 완료했다고 표기하지 않습니다. 읽기 전용 릴리스에는 그 범위를 명시하고, 남은 쓰기 기능은 planned로 유지합니다.
