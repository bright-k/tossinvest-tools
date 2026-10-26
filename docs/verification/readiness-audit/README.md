# 에이전트 사용 준비·Git 시크릿 점검 — 2026-10-26

## 판정

현재 범위는 개인용 readonly R2의 모의 검증을 마친 개발 스냅샷이다. 실행한 독립 서버의 MCP 발견/조회와 CLI는 검증했으며, 새 기능 개발 없이 사용자 등록·재연결 후 제한된 조회 시험을 시작할 수 있다. 현재 채팅에서의 모델 직접 MCP 호출, 실제 Keychain UX·출구 IP·실계좌 헤더 제한·실시장/절전 복귀·launchd 운영은 여전히 사용자 opt-in 확인 대기다. 이 기록은 실제 계좌 운용 검증이나 주문 지원 판정이 아니다.

쓰기 모듈은 없고 toss/mock trading 시작·도구·서비스·최종 전송을 차단한다. 같은 OS UID의 셸 접근을 Keychain·파일 권한만으로 격리했다고 주장하지 않는다. 공개 릴리스의 라이선스·원본 명세 포함 배포 조건도 기존 문서의 확인 대기 항목이다.

## 검토에서 찾아 수정한 항목

1. **복구 조회의 권한 철회(P1)**: WS 재동기화가 최초 계좌 집합을 복사한 뒤 다음 REST 요청의 권한 revision을 다시 검사하지 않았다. 일반 조회와 동일한 최종 dispatch guard를 공통화해 복구의 각 전송·최종 snapshot 게시 전에 검사한다. 허용 중 이미 전송한 요청은 끝날 수 있지만 철회 후 다음 조회는 보내지 않는다. `TestRecoveryStopsNewReadsAfterGrantRevocation`으로 확인한다.
2. **시세 최신성(P2)**: 연결이 살아 있다는 이유로 `stale=false`를 반환했다. freshness 판정 정책이 없는 연결 중 snapshot은 `stale=null`, 단절 뒤 관측 snapshot은 `stale=true`로 바꾸고 원본 data_timestamp를 유지한다. `TestStreamSnapshotFreshnessIsUnknownWhileConnected`로 오래된 원본 시각이 최신으로 보장되지 않음을 확인한다.
3. **오프라인 철회(P2)**: CLI의 `setup accounts --clear`가 로컬 철회 전에 불필요한 upstream 계좌 발견을 호출했다. clear는 owner status/revision과 grant만 사용하도록 고쳤다. `TestClearAccountsNeverCallsUpstreamDiscovery`에서 upstream 발견이 실패하는 상황에도 호출 0건으로 철회를 확인한다.
4. **실행 상태 파일의 Git 제외**: checkout 아래 app-dir를 지정했을 때 mock connection credential·owner socket·lock·로그·복원 거절 DB가 실수로 포함되지 않도록 `.gitignore`를 보강했다. 기존에 추적된 파일에 ignore가 소급 적용되는 것으로 가정하지 않고 인덱스도 별도 검사한다.

수정 근거는 [core 회귀 테스트](../../../internal/core/streams_test.go), [CLI 회귀 테스트](../../../internal/cli/query_test.go), [공통 guard](../../../internal/core/service.go)다. 원본 명세·생성 코드·주문 범위·MCP 도구 수는 바꾸지 않았다.

## 최종 실행 결과

전체 테스트/하위 테스트 **402개**, race detector **402개**가 통과했고 테스트가 있는 패키지 7개에서 실패는 0개다. go vet·명세/문서 검사도 통과했다. 수정한 독립 바이너리의 CLI·SDK MCP(R1 35개/R2 39개 발견, 각각 REST 34개 호출)와 실제 설치 Codex client의 mock 도구 발견을 재확인했다.

이미 실행 중인 서버 프로세스는 이전 바이너리를 계속 사용하므로 사용자가 서버를 재시작하고 MCP를 재연결해야 이번 복구 권한/freshness 수정이 적용된다. 이 점검에서 사용자의 실제 서버를 종료하거나 재시작하지 않았다.

## 시크릿 검사

- [Gitleaks v8.30.1 공식 릴리스](https://github.com/gitleaks/gitleaks/releases/tag/v8.30.1)를 `.tools`에만 준비했고 공식 배포 checksum과 대조했다. archive SHA-256은 `b40ab0ae55c505963e365f271a8d3846efbc170aa17f2607f13df610a9aeb6a5`다.
- 인덱스 snapshot, 추적 파일의 현재 내용+미추적 publish 후보, 모든 Git blob(도달 불가능/과거 staged blob 포함), 모든 ref의 커밋 이력을 각각 검사한다. 실제 Keychain·사용자 앱 데이터·Git credential store를 읽거나 원격으로 소스를 업로드하지 않았다.
- 기본 탐지 규칙과 추가 Toss client ID/실행 파일 이름 검사를 사용한다. scanner에는 `--redact=100`을 명시하고 raw log/report는 임시 private 디렉터리에만 두며, 결과에는 값·일치 원문 없이 파일·행·규칙·분류만 기록한다. 사용자 allow 파일/환경 설정이 검사 규칙을 무력화하지 않도록 기본 규칙 config와 빈 ignore를 명시한다.
- 인덱스/작업 파일의 generic-api-key 탐지는 고정 공식 OpenAPI의 **잘린 공개 토큰 예제 1건**이다. 원본 전체 SHA-256과 정확한 행·규칙이 일치할 때만 공개 예제로 분류한다. 실제 키가 섞인 수정 파일 전체를 무시하지 않는다.
- Git object 전체 검사에서 나온 이전 go.sum의 체크섬 탐지는 모든 행이 Go checksum 형식이고 해당 행이 현재/인덱스 go.sum의 의존성 체크섬과 일치함을 확인했다. API credential로 사용할 수 있는 원문은 기록하지 않는다.
- 최종 판정은 [secrets.json](secrets.json)의 unresolved/supplemental findings와 skipped symlinks를 기준으로 한다. 알려진 공개 예제와 검증된 dependency checksum은 별도 표시한다. 정적 탐지 결과이며 모든 가능한 시크릿 형식의 부재를 수학적으로 보장하는 것은 아니다.

재현:

```sh
./scripts/bootstrap-audit
make audit-secrets
```

기존 검사에 `.tools`, 바이너리, SQLite, 실제 credential 파일이 포함되지 않은 것은 publish 대상에서 제외돼 있기 때문이다. 반대로 ignore된 파일이라도 이미 인덱스에 있거나 Git blob에 들어갔다면 검사에 포함한다.

## 커밋·푸시 판정

현재 인덱스는 앞선 중간 상태이고 실행 진입점·테스트가 미추적 파일로 남아 있다. 이 인덱스만 커밋하면 검증한 실행 프로그램 전체가 포함되지 않는다. 사용자 스테이징은 변경하지 않았다.

최종 작업 파일을 검토해 함께 스테이징하고 `make audit-secrets`, `make check`, `git diff --cached --check`를 다시 통과시킨 개발 스냅샷은 커밋/푸시할 수 있는 상태다. 실제 키·계좌 연결이나 서비스 설치는 코드 커밋의 선행 조건이 아니다. 다만 live-verified·실주문 가능·운영 검증 완료라고 릴리스 표기하지 않는다. 이 요청에서는 커밋·푸시를 실행하지 않았다.

## 실행 근거

- [전체 mock 테스트](tests.jsonl), [race](race.jsonl), [vet](vet.txt)
- [독립 CLI](cli.json), [독립 SDK MCP 발견/호출](mcp.json), [실제 Codex client mock 발견](codex.json)
- [시크릿 검사](secrets.json), [빌드/체크섬](build.txt), [최종 요약](summary.json)
