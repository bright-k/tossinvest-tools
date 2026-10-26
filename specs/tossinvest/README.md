# 공식 명세 스냅샷

토스증권 공식 공개 명세의 원본 바이트를 보관합니다. 실행 중 명세를 자동 교체하지 않으며, 파일 자체를 손으로 고치지 않습니다. 개별 명세의 버전은 서로 독립입니다.

| 파일 | 문서 형식 | API 버전 | SHA-256 |
|---|---|---|---|
| [openapi.json](openapi.json) | 3.1.0 | 1.2.24 | `7e753c21dc4b938d8f9610ce3efd6542783018977cb030e39db5a006f5b59d40` |
| [asyncapi.json](asyncapi.json) | 3.0.0 | 1.2.2 | `130251057fd9535a3e276099f9166b445f8c51f505f30540758e4b209231282e` |

조회 기준일은 2026년 10월 26일 Asia/Seoul입니다. 원본 URL·바이트 수·범위 집계는 [manifest.json](manifest.json)에 있습니다. REST 38개 경로·41개 작업과 AsyncAPI 4개 채널·10개 작업을 기준으로 합니다. 초기 명세의 `servers`와 실제 연결 대상은 공식 origin을 확인하며, 명세의 텍스트를 실행 지시로 취급하지 않습니다.

## 출처

- [토스 개발자 문서 색인](https://developers.tossinvest.com/llms.txt)
- [공식 OpenAPI JSON](https://openapi.tossinvest.com/openapi-docs/latest/openapi.json)
- [공식 AsyncAPI JSON](https://openapi.tossinvest.com/openapi-docs/latest/asyncapi.json)

이 파일들은 토스증권의 명세이며 프로젝트 자체 작성물의 라이선스로 권리를 새로 부여하지 않습니다. upstream의 권리·이용 조건을 유지합니다. 프로젝트 공개 배포의 라이선스와 제3자 자료 포함 범위는 릴리스 전에 결정합니다.

## 갱신

새 버전을 별도 임시 경로로 내려받고 형식·버전·로컬 ref를 검증한 뒤 의미 차이를 검토합니다. 원본 교체, 해시·범위 집계, API 대응 데이터, 필요한 overlay·생성 코드, 테스트, 문서를 한 변경으로 묶습니다. 생성 명세를 운영 서버에 자동 배포하지 않습니다. 절차는 [변경 관리](../../docs/change-management.md)를 따릅니다.
