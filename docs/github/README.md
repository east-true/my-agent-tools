# GitHub 명령어 안내

`tools github`의 명령별 사용법입니다. 예시는 한국어 본문과 소문자 영어 제목을 사용합니다.

## 명령어 목록

| 명령 | 하는 일 | 변경 범위 |
|---|---|---|
| [setup](setup.md) | 기존 라벨·유형을 가져와 프로젝트 설정 저장 | 로컬 설정 파일 |
| [context](context.md) | 저장소 라벨·유형·적용 정책 조회 | 읽기 전용 |
| [ci failures](ci/failures.md) | 실패한 CI job·step·annotation·오류 자료 수집 | 읽기 전용 |
| [ci rerun](ci/rerun.md) | 실패 job 재실행·새 회차 완료 대기·재실패 자료 수집 | GitHub Actions 재실행 |
| [dependabot list](dependabot/list.md) | 의존성 보안 경고 목록·패치 버전 조회 | 읽기 전용 |
| [dependabot view](dependabot/view.md) | 특정 보안 경고의 설명·참고 링크·무시 사유 조회 | 읽기 전용 |
| [issue create](issue/create.md) | 이슈 생성·담당자 지정·Development 브랜치 연결 | GitHub 및 별도 로컬 워크트리 생성 |
| [issue branch](issue/branch.md) | 기존 이슈의 연결 브랜치·워크트리 생성·재개 | GitHub 및 별도 로컬 워크트리 생성 |
| [pr create](pr/create.md) | 원격 변경을 확인하고 PR 생성·라벨 적용 | GitHub |
| [pr submit](pr/submit.md) | 기존 커밋 푸시·PR 재사용/생성·검사 대기·자료 수집 | Git 원격 푸시 및 GitHub |
| [pr reviews](pr/reviews.md) | 제출된 리뷰 본문·미해결 스레드·위치·대화 수집 | 읽기 전용 |
| [pr inspect](pr/inspect.md) | PR 상태·리뷰·검사·실행 ID·실패 자료·변경분 조회 | GitHub 읽기, 선택적 로컬 상태·원문 저장 |
| [pr delta](pr/delta.md) | 기준 커밋 이후 현재 head의 변경 파일 조회 | 읽기 전용 |
| [pr merge](pr/merge.md) | 검사 완료 대기·머지 확인·해당 PR 브랜치와 워크트리 정리 | GitHub 및 Git 정리 |
| [branch cleanup](branch/cleanup.md) | 종료된 작업의 워크트리·브랜치 판별·정리 | 미리보기 또는 워크트리 제거·참조 삭제 |

## 기본 흐름

```sh
tools github setup
tools github issue create --prefix feat --title "add login" --body-file issue.md --json
# 반환된 branch.worktree_path로 이동
# 구현·검증 후 Git으로 커밋·푸시
tools github pr create --prefix feat --title "add login" --body-file pr.md --json
# 리뷰가 제출되면 요청 원문·위치·대화 확인
tools github pr reviews --number 123 --json
tools github pr merge --number 123 --json
# 다른 종료 작업도 정리하려면 주 작업 폴더에서 후보 확인
tools github branch cleanup --json
```

`setup`은 프로젝트 설정을 처음 저장하거나 갱신할 때 사용합니다.
생성 명령이 필요한 GitHub 목록을 직접 조회하므로 매번 `context`나 `setup`을 먼저 실행할 필요는 없습니다.

## 설치와 인증

저장소 루트에서 빌드한 바이너리를 PATH에 둡니다.

```sh
go build -o tools ./cmd/tools
# Windows용 파일 이름
go build -o tools.exe ./cmd/tools
```

PowerShell에서 현재 폴더의 바이너리를 실행할 때는 `./tools.exe`를 사용합니다.
빌드에는 Go 1.26 이상이 필요하며 실행 시 Go 런타임은 필요하지 않습니다.

GitHub API 인증은 `GH_TOKEN` → `GITHUB_TOKEN` → 기존 `gh auth token` 순으로 확인합니다.
환경변수에 토큰을 제공하면 `gh` 설치는 필수가 아닙니다.
GitHub CLI로 인증하려면 `gh auth login`을 사용합니다.
인증 토큰은 프로젝트 설정이나 명령 결과에 저장하지 않습니다.
현재 지원 호스트는 `github.com`입니다.

`--repo OWNER/REPO`를 생략하면 Git의 `origin`에서 저장소를 찾습니다.
Git 밖에서는 명시적인 `--repo`를 사용합니다.
워크트리 생성과 브랜치 정리에는 Git이 필요합니다.
원격 브랜치 삭제는 Git에 설정된 전송 인증도 필요합니다.

## 프로젝트 정책

기본 설정 경로는 Git 루트의 `.tools.json`이며, Git 밖에서는 현재 폴더입니다.
`--config FILE`로 다른 파일을 선택합니다. `branch cleanup`, `ci failures`, `ci rerun`, `dependabot list / view`, `pr reviews`, `pr inspect`, `pr delta`, `pr merge`는 자체 옵션을 사용하며 `--config`를 받지 않습니다.

```json
{
  "github": {
    "body_language": "any",
    "label_map": {"feat": ["type: feature", "enhancement"]},
    "issue_type_map": {"feat": ["Feature", "Task"]}
  }
}
```

기본 본문 언어 검사는 `ko`입니다. 다른 언어의 본문은 `body_language: "any"`로 허용합니다.
배열은 우선순위 후보이며 자동 매핑은 일치하는 라벨 하나·이슈 유형 하나를 선택합니다.
기본 후보는 코드에 있고 설정한 prefix만 덮어씁니다. 빈 배열은 해당 매핑을 끕니다.
라벨은 먼저 이름을 직접 대조하고, 직접 일치가 없으면 `type:`, `type/`, `kind:`, `kind/`, `category:` namespace도 대조합니다.
이슈 유형은 활성화된 기존 이름과 대조합니다. 이름 비교는 대소문자를 구분하지 않습니다.
GitHub 라벨·유형을 새로 생성하지 않습니다.
자연어 `AGENTS.md`를 정책으로 해석하지 않습니다.

지원 prefix:

```text
feat, fix, refactor, docs, ci, test, chore, build, perf, style, revert
```

## 출력과 오류

`--json`은 자동화에서 사용할 구조화된 결과를 출력합니다.
`--help`와 `-h`는 인증 없이 도움말을 표시합니다.
각 명령 문서의 결과 필드를 확인하세요.

| 종료 코드 | 의미 |
|---|---|
| `0` | 명령 완료 또는 정상적인 미리보기 |
| `1` | 인증·API·Git·설정 저장 등 실행 오류 또는 생성 후 일부 작업 실패 |
| `2` | 잘못된 옵션·입력·설정 읽기/검증·매핑 |

생성 결과가 `partial`이면 이미 생성된 URL을 확인하고 해당 항목에서 이어갑니다.
응답을 잃었다면 GitHub 상태를 먼저 확인합니다. 생성 요청은 자동 재시도하지 않습니다.

## 조회 재사용과 호출 제한

`ci failures`, `ci rerun`, `pr inspect`, `pr submit`, `pr merge`는 완료된 회차의 완전한 실패 자료를 공유합니다. 저장소·실행 ID·attempt·SHA·완료 상태·결론·annotation 옵션·로그 상한이 같아야 재사용합니다. 실행 메타데이터는 인증된 API로 다시 확인하며 조회 실패·권한 오류에서 캐시를 대신 반환하지 않습니다. 부분 자료·진행 중 회차는 캐시하지 않습니다. `pr inspect --state-file`의 변경분 출력은 별도로 유지합니다.

캐시는 기본으로 주 저장소의 `.tools/state/github-cache`에 저장하며 연결 워크트리도 공유합니다. Git 밖에서는 현재 폴더를 기준으로 합니다. `TOOLS_GITHUB_CACHE_DIR`로 경로를 바꾸고 `TOOLS_GITHUB_CACHE=0`으로 자료 재사용을 끌 수 있습니다. 인증 정보의 해시별로 분리하고 토큰 자체는 저장하지 않습니다. CI 자료는 30일, REST 응답은 1시간 동안 재사용 대상으로 유지합니다. 새 저장 시 해당 인증·자료 종류별 관리 파일을 최근 200개로 정리합니다. 캐시 저장 실패·손상은 조회 결과를 바꾸지 않고 정상 수집으로 처리합니다. `ci rerun`의 요청 체크포인트는 캐시와 별개입니다.

지원하는 REST GET은 ETag 또는 Last-Modified로 조건부 요청을 보내며, 서버가 인증된 요청에 `304`를 반환하면 저장한 본문과 페이지 링크를 사용합니다. 다른 자격 증명·URL·Accept·API 버전은 섞지 않습니다. 요청에 붙은 인증 정보가 없으면 HTTP 응답을 캐시하지 않습니다. GraphQL POST에는 조건부 GET을 적용하지 않습니다.

호출 제한은 `Retry-After`와 reset 시각에 맞춰 기다리고, 별도 지침이 없는 secondary limit은 1분부터 대기 시간을 늘립니다. 일시적인 500·502·503·504 오류도 GET에만 최대 두 번 재시도합니다. 일반 권한 오류와 POST·PUT·PATCH·DELETE는 재시도하지 않습니다. 서버의 `X-Poll-Interval`도 반영하며 호출 컨텍스트·명령 제한 시간을 넘겨 계속 기다리지 않습니다. [GitHub 공식 지침](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api).

API 요청·다운로드 감소는 전체 에이전트 토큰 절감률과 별개입니다. 기존 출력 형식을 유지하며 긴 출력의 토큰 검증은 아래 `--compact` 규칙을 적용합니다.

## 간결한 출력

`pr reviews`, `ci failures`, `ci rerun`은 `--compact`로 구조화된 간결한 출력을 선택합니다. `pr inspect`, `pr submit`, `pr merge`는 기본으로 활성화하며 `--compact=false`로 전체 출력을 사용합니다. `pr delta`는 기본 patch 생략으로 출력량을 줄이고 `--include-patch`로 선택합니다.

리뷰 본문·diff hunk·진단 `message`는 길어도 그대로 반환합니다. 리뷰의 중복 위치·시각 메타데이터를 줄이면서 코드 수정에 필요한 원문을 유지합니다. 긴 오류 설명의 `raw_details`·`summary`·`text`와 긴 CI 로그는 일부를 생략할 수 있으며 `*_truncated`, `lines_truncated`, `excerpts`, `omitted_lines`로 표시합니다. 해당 자료의 원문이 필요하면 `--compact=false`를 사용하거나 원문 파일을 확인합니다. 원문은 `.tools/state/evidence/<SHA256>.json`에 저장하고 `evidence_file`, `evidence_sha256`을 반환합니다. `--artifact-dir DIR`로 경로를 바꿉니다.

파일 참조와 SHA-256까지 포함한 JSON의 바이트 수와 토큰 수가 모두 감소하는 경우에만 축소합니다. 토큰은 로컬에 포함된 인코더로 계산하며 모델·네트워크를 호출하지 않습니다. `--token-encoding o200k_base`가 기본이며 `cl100k_base`도 지원합니다. 사용하는 모델의 인코딩과 다르면 해당 모델의 토큰 수를 보장하지 않습니다. [고정 자료의 출력 토큰 측정](../benchmarks/github/pr/reviews.md)을 참고하세요.

줄 번호는 해당 evidence 구간의 1 기반 위치이며 전체 job의 위치는 `occurrences`를 함께 확인합니다. 원문 저장·보관 정리에 실패하면 실제 실행 결과를 전체 출력으로 유지하고 종료 코드 `1`을 반환합니다. 출력 축소는 자료 수집의 `complete`나 CI·리뷰의 실제 상태를 바꾸지 않습니다.

원문 파일은 축약이 실제 적용될 때 기본 30일(`--artifact-retention 720h`)·최근 200개(`--artifact-limit 200`)로 관리합니다. 각각 `0`이면 해당 제한을 해제합니다. 현재 반환하는 파일은 보존하고, 파일명과 내용의 SHA-256이 일치하는 관리 파일만 삭제합니다. 다른 파일·디렉터리·심볼릭 링크는 삭제하지 않습니다. 기간 경과만으로 자동 실행되는 정리는 없습니다. 장기 참조 자료는 별도로 보관하세요. `--state-file`은 이 정리 대상에 포함되지 않습니다.

**출력 바이트 감소가 에이전트 총 토큰 절감을 보장하지는 않습니다.** 단순 메타데이터는 직접 조회도 같은 짧은 출력을 만들 수 있습니다. [최신 증분 조회 측정](../benchmarks/github/pr/delta.md)은 기준 SHA 검증을 포함한 기본 patch 생략 시나리오에 한정됩니다.

## 관련 문서

- [에이전트용 빠른 사용법](../agent-usage.md)
- [설정 예제](../../examples/github/tools-config.json)
- [명령어별 토큰 측정](../../README.md#token-usage-measurements)
- [한국어 프로젝트 안내](../README.ko.md)
