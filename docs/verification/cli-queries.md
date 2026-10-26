# CLI 업무 조회 확장 검증 — 2026-10-26

후속 검토의 복구 권한/freshness/오프라인 철회 보완과 최신 빌드는 [준비 상태 재점검](readiness-audit/README.md)을 따른다. 이 문서의 399개 결과는 CLI 확장 당시 기록이다.

계좌 목록·보유 자산·현재가를 CLI에서 조회하도록 구현했다. 실제 키·계좌·사용자 기본 app-dir는 사용하지 않고 임시 mock 서버와 mock credential만 검증에 사용한다. 기존 서버에 인증된 MCP 클라이언트로 연결하므로 REST 토큰 관리·계좌 grant·scope·철회·오류 처리는 기존 서비스와 동일하다.

## 명령

서버가 실행 중일 때 사용한다. 실제 연결은 사용자가 준비한 서버에 대해 `--backend=toss`로 바꾼다. 이 문서의 검증은 mock 전용이다.

```sh
./bin/tossinvest-tools accounts --backend=mock --json
./bin/tossinvest-tools holdings --account-alias=primary --backend=mock --json
./bin/tossinvest-tools holdings --account-alias=primary --symbol=005930 --backend=mock --json
./bin/tossinvest-tools prices --symbols=005930,AAPL --backend=mock --json
```

`--account-alias`는 필수이며 첫 계좌를 자동 선택하지 않는다. `--symbol`은 보유 자산의 선택 필터, `--symbols`는 현재가의 필수 콤마 구분 값(최대 200개)이다. 기본 출력과 `--json` 출력은 모두 서버의 data/meta JSON이다. 마스킹 계좌·Decimal 문자열·원본 필드·null·시각·추가 숫자의 정밀도를 보존한다.

서버가 없으면 exit 3, 입력 오류 2, 인증/금지·철회 계좌 4, 준비 미완료 5, upstream 조회 실패는 원본 상세 code와 함께 exit 1이다. 업무·필드 검증 실패는 stderr JSON만 출력한다. 알 수 없는 옵션 등 CLI 인자 파싱 오류는 기존 사용 안내와 exit 2를 유지한다. CLI는 Toss 키를 읽거나 별도 토큰을 발급하지 않으며 서버 없는 fallback을 제공하지 않는다.

## 구현과 검증 경계

- [CLI 명령 매핑](../../internal/cli/query.go)은 세 개의 고정 도구에만 연결한다. [MCP client](../../internal/runtime/query.go)는 기존 ConnectionCredential과 공식 SDK v1.8.0을 사용한다. 새로운 서버 라우트·도구·권한 우회·의존성·DB migration은 없다.
- [CLI 통합 테스트](../../internal/cli/query_test.go): 미선택 계좌 빈 집합·시장 조회, 계좌 선택 후 허용 목록/보유 자산, 미허용·철회 거부, 마스킹·credential 비노출, 설정 backend, 잘못된 credential, 입력 오류와 서버 미실행.
- [client 계약 테스트](../../internal/runtime/query_test.go): 필터 및 서버 계좌 헤더 전달, upstream 429/code/requestId/Retry-After와 새 error data 보존, origin/인증 오류, 원본 큰 정수·Decimal·null 보존, 손상된 응답/비허용 도구 거부.
- [실행 파일 검사](../../scripts/check-cli.py): 별도 CLI 프로세스와 임시 mock 서버로 세 조회를 수행하고 PTY로 선택/철회한 계좌 정책, stdout/stderr·exit code, 서버 미실행을 확인한다. 기존 정상/강제 종료 credential 검증도 유지한다.

서버가 structuredContent와 동일하게 제공하는 JSON text 복사본을 UseNumber로 읽는다. SDK의 any 변환으로 인한 큰 정수 반올림을 피하고 조회 결과를 임의로 요약하지 않는다. HTTP 인증은 각 MCP 요청에 적용하며 credential을 다른 origin/redirect/proxy로 보내지 않는다. 실제 Keychain/실계좌를 조회한 기록은 아니다.

## 최종 결과

일반 테스트와 race detector에서 각각 테스트/하위 테스트 **399개**, 테스트가 있는 패키지 7개가 통과했으며 실패는 0개다. go vet·명세/문서 대응 검사도 통과했다. 최종 실행 파일의 독립 CLI 프로세스로 accounts·holdings·prices, 허용/철회 계좌, 입력/인증 오류·서버 미실행을 확인했다. 실행 파일과 bin/SHA256SUMS를 갱신했고 실제 키·실계좌·서비스 설치는 사용하지 않았다.

## 재현과 결과

```sh
./scripts/go test ./...
./scripts/go test -race ./...
./scripts/go vet ./...
make build
make cli-check
make check
```

이번 확장의 실행 결과는 아래에 별도로 보관한다. 초기 readonly R2의 기존 [검증 기록](README.md)은 당시 결과이며 현재 확장 빌드와 구분한다.

- [전체 테스트](cli-queries/tests.jsonl)
- [race 결과](cli-queries/race.jsonl)
- [vet 출력](cli-queries/vet.txt)
- [독립 CLI 프로세스 결과](cli-queries/cli.json)
- [빌드·체크섬](cli-queries/build.txt)
- [최종 요약](cli-queries/summary.json)
