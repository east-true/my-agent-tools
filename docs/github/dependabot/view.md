# `tools github dependabot view`

특정 Dependabot 보안 경고의 설명·참고 링크·패치 정보를 읽습니다. 경고 번호는 [dependabot list](list.md)로 찾습니다.

```sh
tools github dependabot view --number 7 --json
tools github dependabot view --repo OWNER/REPO --number 7 --json
```

`--number`는 Dependabot 경고 번호이며 이슈·PR 번호와 별개입니다.

## 옵션

| 옵션 | 의미 |
|---|---|
| `--number NUMBER` | 양의 경고 번호, 필수 |
| `--repo OWNER/REPO` | 생략하면 Git `origin`에서 저장소 추론 |
| `--json` | 구조화된 JSON 출력 |

읽기 전용 명령이며 설정 파일·라벨·이슈 유형을 조회하지 않습니다. 경고 상태 변경, 이슈 생성, 의존성 수정은 별도 작업입니다.

## JSON 결과

`status`, `repo`, `alert`를 반환합니다. `alert`에는 다음 필드가 포함됩니다.

- `number`, `state`, `url`: 경고 번호·상태·GitHub 링크
- `package`, `ecosystem`, `manifest_path`, `scope`, `relationship`: 영향받는 의존성. `relationship`은 GitHub가 제공할 때만 포함
- `severity`, `summary`, `ghsa_id`, `cve_id`: 심각도·요약·취약점 식별자. CVE가 없으면 `cve_id` 생략
- `vulnerable_version_range`, `first_patched_version`: 영향 버전 범위·최초 패치 버전. 패치 정보가 없으면 `first_patched_version: null`
- `description`, `references`, `dismissed_reason`, `dismissed_comment`: 설명·참고 링크·무시 사유. 값이 있을 때 포함

패치 버전은 경고의 `security_vulnerability`에서 가져옵니다. 같은 advisory에 나열된 다른 릴리스 계열의 패치 버전과 구분하며, 프로젝트에서 바로 적용할 수 있는 업데이트 버전을 계산하지는 않습니다.

## 인증과 오류

기존 인증 순서인 `GH_TOKEN` → `GITHUB_TOKEN` → 선택적 `gh auth token`을 사용합니다. Fine-grained 토큰은 대상 저장소의 **Dependabot alerts: read** 권한이 필요합니다. Classic 토큰/OAuth 앱 토큰은 `security_events`, 공개 저장소에만 사용한다면 `public_repo` scope를 사용할 수 있습니다. [GitHub 공식 API 문서](https://docs.github.com/en/rest/dependabot/alerts#get-a-dependabot-alert).

`403`·`404`는 실제 API 오류로 반환합니다. 저장소·경고 접근 권한, Dependabot alerts 활성화 여부, 토큰 권한을 확인하세요. `404`만으로 저장소 부재·권한 부족·경고 부재를 단정하지 않습니다.

종료 코드는 성공 `0`, 인증·API·저장소 조회 오류 `1`, 잘못된 옵션·경고 번호 `2`입니다. `--json` 실행 오류는 `status: "error"`와 `error`를 반환합니다.

[공통 안내](../README.md) · [경고 목록](list.md)
