# my-agent-tools

[English](../README.md)

에이전트의 반복 작업을 자동화하는 범용 CLI입니다. 바이너리는 `tools`, 작업은 서브커맨드로 구분합니다. 현재 구현은 **prefix 기반 GitHub 이슈·브랜치·PR 자동화**입니다. 커밋·푸시는 기존 Git 명령으로 처리합니다.

```text
tools github issue create --prefix feat --title "Add login / OAuth!" --body-file issue.md
```

이 한 명령으로 기존 라벨·유형과 Markdown 형식을 조회하고, `feat: Add login / OAuth!` 제목과 담당자 `me`로 이슈를 생성합니다. 번호가 123이면 `123-feat-add-login-oauth` 브랜치를 생성해 Development 항목에 연결하고, 현재 origin이 대상 저장소이면 로컬에서 그 브랜치로 전환합니다. 라벨·유형·브랜치 이름은 직접 작성할 필요가 없습니다.

계획만 필요하면 같은 명령에 `--dry-run --json`을 추가합니다. 생성 명령이 필요한 목록 조회와 검증을 처리하므로 `context`는 선택 사항입니다. 오류나 필요한 정보의 누락이 없으면 반환된 계획·결과를 사용하고 목록을 다시 조회하지 않아도 됩니다. Git 작업 디렉터리 밖에서는 `--repo OWNER/REPO`를 지정합니다.

## 설치

빌드는 Go 1.26 이상이 필요합니다. 실행할 때 Go 런타임은 필요하지 않습니다.

```text
go build -o tools ./cmd/tools
go build -o tools.exe ./cmd/tools
```

첫 명령은 Linux/macOS, 두 번째는 Windows용입니다. 바이너리 디렉터리를 PATH에 추가하면 `tools`로 실행합니다. PowerShell에서 현재 디렉터리의 파일을 실행할 때는 `./tools.exe`를 사용합니다.

GitHub API는 [go-github](https://github.com/google/go-github) 라이브러리로 직접 호출합니다. `gh` 설치는 필수가 아닙니다. 인증은 `GH_TOKEN` → `GITHUB_TOKEN` 환경변수 순이며, 둘 다 없으면 선택적으로 설치된 `gh auth token`으로 기존 로그인을 재사용합니다. 토큰은 입력·설정·결과에 저장하지 않습니다.

`git`은 현재 origin·브랜치 조회와 로컬 브랜치 전환에 사용합니다. `--repo OWNER/REPO`와 `--no-checkout`을 지정하면 이슈와 연결된 원격 브랜치를 `git` 없이 생성할 수 있습니다. PR도 `--repo`와 JSON의 `head`를 명시하면 `git`이 필요하지 않습니다. 현재 지원 호스트는 `github.com`입니다.

## 명령

```text
tools github issue create --prefix fix --title "handle duplicate requests" --body-file issue.md --json
tools github issue create --file issue.json --dry-run --json
tools github issue branch --number 123
tools github pr create --prefix fix --title "handle duplicate requests" --body-file pr.md --json
```

옵션이 필요할 때만 `tools github issue create --help`처럼 해당 명령의 도움말을 확인합니다. 각 단계의 도움말에는 사용 가능한 prefix 목록이 표시됩니다.

| 옵션 | 동작 |
|---|---|
| `--prefix` | `feat`, `fix`, `refactor`, `docs`, `ci`, `test`, `chore`, `build`, `perf`, `style`, `revert` 중 선택 |
| `--title` | 영어 제목. 대소문자를 포함한 입력 내용은 보존하고 선택한 prefix만 추가 |
| `--body-file FILE` | UTF-8 Markdown 본문. `-`이면 표준입력 |
| `--repo OWNER/REPO` | 대상 저장소 지정. 기본은 현재 저장소의 origin |
| `--file FILE` | UTF-8 JSON 입력. `-`이면 표준입력 |
| `--dry-run` | 조회·사전 검증 후 등록될 내용을 JSON으로 출력. 원격 쓰기 없음 |
| `--json` | 구조화된 결과 출력 |
| `--config FILE` | 정책 파일 지정. 기본은 현재 Git 루트의 `.tools.json`, Git 밖에서는 현재 디렉터리 |
| `--no-checkout` | 이슈의 연결된 원격 브랜치만 생성하고 로컬 전환 생략 |
| `--no-branch` | 이슈만 생성 |

`context`는 필요할 때 목록을 확인하는 보조 명령입니다. 생성 명령이 기존 라벨·유형·본문 형식을 직접 조회하므로 사전에 실행할 필요가 없습니다. 기본 본문은 한국어입니다. 제목·본문 정보만 입력하면 됩니다.

저장소의 Markdown 이슈·PR 템플릿을 조회해 prefix와 맞는 파일이나 기본 템플릿을 선택합니다. 기존 섹션 제목을 사용하면서 작성한 내용을 채우며, 템플릿에 대응하지 않는 내용도 보존합니다. `{{body}}`가 있으면 그 위치에 본문을 넣습니다. 템플릿이 없으면 기본 형식으로 작성합니다. 현재 GitHub GraphQL이 제공하는 Markdown 템플릿을 사용하며 YAML issue form은 해석하지 않습니다.

## 이슈 입력

```json
{
  "prefix": "fix",
  "title": "handle duplicate requests",
  "summary": "동일 요청이 중복 처리되는 문제를 수정합니다.",
  "changes": ["멱등성 키 처리 개선"],
  "acceptance": ["동일 키의 요청은 한 번만 처리된다"]
}
```

JSON은 구조화된 내용을 전달하는 대안입니다. `--title/--body-file`과 함께 사용할 수 없습니다. 제목 내용은 대소문자를 포함해 입력 그대로 보존하며 선택한 prefix만 붙여 `<prefix>: <summary>`로 전달합니다. 이미 prefix가 있는 제목도 그대로 받습니다. 제목은 영어 대소문자와 숫자·ASCII 기호·공백만 허용하며 영문자나 숫자를 최소 하나 포함해야 합니다. 한글·앞뒤 공백 등 규칙 위반은 등록 전에 오류로 알리고, 자동 번역·제목 생성은 하지 않습니다. `summary`, `changes`, `acceptance`로 본문을 구성하며 작성한 내용에 한글이 포함되는지 검사합니다. 번역 품질이나 의미를 판단하지는 않습니다. 담당자는 항상 인증된 사용자(`@me`)입니다.

저장소 기본 브랜치의 원격 커밋에서 `<issue-number>-<prefix>-<slug>` 브랜치를 만들고 이슈의 Development 항목에 연결합니다. slug는 제목에서 prefix를 빼고 소문자로 변환한 뒤 특수기호·공백을 `-`로 바꾼 값입니다. 제목 자체는 변경하지 않습니다. 연속된 `-`와 양 끝의 `-`를 정리하고, 파일 이름 길이를 위해 최대 180자로 제한합니다. 현재 origin이 대상과 다르거나 Git 작업 디렉터리가 아니면 원격 브랜치만 생성합니다. 로컬 전환은 Git의 충돌 검사를 따르고 기존 브랜치를 강제로 덮어쓰지 않습니다.

라벨·유형은 prefix 매핑으로 자동 지정합니다. 본문을 의미 분석해 다른 라벨을 추가하지 않습니다. 예외적인 수동 지정이 필요할 때만 JSON의 `labels`, `issue_type`을 사용합니다. 지정한 이름이 없으면 등록 전에 오류로 처리하며, 담당자를 바꾸는 입력은 없습니다.

## PR 입력

```json
{
  "title": "fix: handle duplicate requests",
  "summary": "동일 요청을 한 번만 처리하도록 수정합니다.",
  "changes": ["멱등성 키 확인을 트랜잭션에 포함"],
  "verification": [
    {"command": "go test ./...", "result": "passed"},
    {"command": "integration tests", "result": "not-run", "details": "로컬 DB 미기동"}
  ],
  "issue": 123,
  "head": "123-fix-duplicate-requests",
  "draft": true
}
```

PR은 현재 브랜치의 이슈 번호도 자동으로 읽습니다. `123-fix-duplicate-requests`에서 실행하면 `issue`를 입력하지 않아도 `Closes #123`이 추가됩니다. JSON의 `issue`로 명시할 수도 있습니다. 연결·종료 동작은 [GitHub 문서](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/linking-a-pull-request-to-an-issue)를 따릅니다.

`base`는 저장소 기본 브랜치, `head`는 현재 브랜치가 기본값입니다. 이슈가 있으면 `<issue-number>-<type>-<slug>`, 없으면 `<type>/<slug>` 이름을 검증합니다. `owner:branch` 형식의 fork head도 입력할 수 있습니다. 원격 head가 base보다 앞선 커밋을 가져야 하므로 **먼저 커밋·푸시해야 합니다**. 자동 커밋·푸시는 하지 않습니다.

`verification`은 작성자가 제공한 기록이며 명령을 실행하지 않습니다. `result`는 `passed`, `failed`, `not-run` 중 하나이며 실패·미실행은 `details`가 필요합니다. 기록을 생략하면 미실행으로 표시합니다.

직접 작성한 본문은 `body`로 입력합니다. 구조화된 본문 필드(`summary`, `changes`, `acceptance`, `verification`)와 함께 사용하면 오류입니다. 직접 작성하는 PR 본문에는 변경 내용과 실제 검증 결과를 포함하세요.

예시: [이슈](../examples/github/issue.json), [PR](../examples/github/pr.json).

에이전트에게 전달할 짧은 사용 지침은 [빠른 사용법](agent-usage.md)에 있습니다.

## 프로젝트 정책

프로젝트 루트의 `.tools.json`:

```json
{
  "github": {
    "body_language": "ko",
    "label_map": {"feat": ["type: feature", "enhancement"]},
    "issue_type_map": {"feat": ["Feature", "Task"]}
  }
}
```

후보 순서대로 기존 이름과 대소문자 구분 없이 비교해 첫 일치를 선택합니다. 일반적인 `type: bug`, `kind/feature` 같은 라벨 이름도 인식합니다. 설정한 prefix만 기본 매핑을 대체합니다. 표준 GitHub 라벨을 사용하는 저장소에는 설정이 필요하지 않습니다. `body_language: "any"`는 한국어 검사 없이 영문 섹션 제목을 사용합니다. 자연어 `AGENTS.md`를 자동 해석하지 않으므로 추가 프로젝트 규칙은 설정에 반영하세요.

기본 라벨은 `fix` → `fix`, `bug`; `feat` → `feat`, `feature`, `enhancement`; `docs` → `docs`, `documentation` 순입니다. 이슈 유형은 `fix` → `Bug`, `Task`; `feat` → `Feature`, `Task`; 나머지는 `Task`입니다. 일치가 없으면 생략 사유를 보고합니다. 새 라벨·유형은 생성하지 않습니다. 유형 조회의 404는 미지원 상태로 보고하고, 다른 조회 오류는 중단합니다.

## 결과와 실패 처리

기본 출력은 생성 주소, 자동 지정한 라벨·유형, 브랜치 연결·전환 결과입니다. JSON에는 `selection`과 `branch`도 포함됩니다. `status`는 `ok`, `planned`, `created`, `partial`, `error` 중 하나입니다.

- 종료 코드 `0`: 성공.
- 종료 코드 `1`: 인증·API·원격 상태 오류 또는 등록 후 메타데이터 적용 실패.
- 종료 코드 `2`: 인자·JSON·프로젝트 설정 오류.

PR 등록과 라벨 지정은 별도 요청입니다. 생성 후 라벨 적용이 실패하거나 GitHub가 이슈 담당자·유형·라벨을 누락하면 `partial`과 생성된 주소를 보존합니다. 기존 항목을 수정하고 다시 생성하지 마세요. 생성 요청은 자동 재시도하지 않습니다. 응답을 받지 못하면 실제 생성 여부를 먼저 확인해야 합니다. 중복 방지용 영구 상태는 저장하지 않습니다.

이슈 생성 후 브랜치 생성·연결·로컬 전환이 실패해도 기존 이슈와 확인된 브랜치 정보를 보존합니다. `tools github issue branch --number 123`으로 브랜치 작업만 이어갈 수 있습니다. 같은 저장소에서 같은 이슈에 이미 연결된 브랜치는 재사용하며, 연결되지 않은 동명의 브랜치는 덮어쓰지 않습니다.

## 토큰 사용량 실측

Git 작업 폴더와 양쪽 사용 안내를 제공하고 이슈·브랜치 계획 작성만 각 3회 비교했습니다. 입력·출력 합계는 `gh` 직접 사용 평균 46,630토큰, `tools` 평균 46,279토큰으로 0.75% 차이였습니다. 캐시를 제외한 입력은 각각 16,035와 16,063토큰으로 사실상 같았습니다. 두 방식 모두 쉘 호출 2회였으며 `gh`는 여러 API 명령을 한 번에 실행했습니다. 이 소규모 실험으로 유의미한 토큰 절감을 입증하지는 못했고, 실제 이슈·브랜치·PR 생성은 측정하지 않았습니다. [재측정 결과와 프롬프트](benchmarks/github-token-usage-guided.json), [사용 안내 없이 진행한 이전 결과](benchmarks/github-token-usage.json)를 참고하세요.

[추가 후보 검증](benchmarks/github-candidate-validation.json)은 공개 GitHub 응답을 고정해 모델 실험 18회를 수행했습니다. CI 오류 추출은 실패 단계 로그를 그대로 읽는 기준보다 총 토큰을 13.7% 줄였고, 리뷰 목록은 1.2% 감소, PR 증분 조회는 3.0% 증가했습니다. 리뷰·커밋 비교 필터는 `gh --jq`로도 같은 정보를 반환합니다. `scripts/benchmarks`의 오프라인 프로토타입이며, 실제 `tools github` 명령으로 추가한 기능은 아닙니다.

[후속 CI 검증](benchmarks/github-ci-internal-validation.json)은 강화된 `gh + rg` 기준과 내부 파서를 각각 3회 추가 측정했습니다. 평균 총 토큰은 `gh + rg` 31,658, 내부 파서의 결과를 에이전트에 전달한 방식 30,244로 4.5% 차이였습니다. 내부 코드만 실행했을 때는 CI·리뷰·증분 조회의 정해진 정답을 모두 모델 호출 없이 반환했습니다. 이 구성 요소의 모델 토큰은 0이지만, 외부 에이전트를 호출하는 비용은 별도입니다. 경계 사례 검사 21개를 통과했으며, `python3 scripts/benchmarks/github_candidates.py ci-facts LOG_FILE`로 지원하는 Go 컴파일 정보를 추출할 수 있습니다. 지원하지 않는 형식은 원래 로그를 보존합니다.

[반복 코드 수정 검증](benchmarks/github-workflow-validation.json)은 실제 Go 코드 수정 두 건을 방식별 3회 수행하고 독립 테스트로 확인했습니다. 평균 총 토큰은 로그를 읽는 에이전트 96,631, 보고서·코드를 미리 받은 에이전트 94,441, 수정 코드만 반환하고 내부 로직이 적용·테스트한 방식 30,362였습니다. 마지막 방식의 총 처리 토큰은 68.6% 적었지만, 캐시 적중이 0이어서 캐시 제외 입력은 9,658에서 30,018로 증가했습니다. 비용 절감은 입증하지 못했습니다. 수정 18건은 모두 검증을 통과했고, 모든 방식에 동일한 내부 게이트를 적용해 CI 상태 8개 중 중복·통과 상태 6개는 모델 호출 없이 처리했습니다.

## 구조화된 CI 보고서 실험

CI는 `go test -json` 결과에서 실패 테스트와 패키지·빌드 오류를 버전이 있는 `ci-report.json`으로 생성합니다. 각 OS의 보고서는 Gitleaks를 통과한 경우에만 `go-ci-<os>-<attempt>` artifact로 업로드합니다. 보고서 생성과 테스트는 로컬에서 확인했으며 GitHub의 업로드 실행은 아직 검증하지 않았습니다.

실험 스크립트는 Python 3.9 이상을 사용하며 Go 바이너리와 별개입니다. 다음 명령은 새 실패·변경된 실패·해결된 실패와 필요한 소스 문맥을 준비합니다. `EXPECTED_CI_SHA`는 보고서를 만든 CI 리비전이며, 보고서를 조회하는 것만으로 처리 완료를 기록하지 않습니다.

```sh
python scripts/benchmarks/workflow_reports.py prepare --report artifacts/ci-report.json --state .tools/state/ci.json --expected-revision EXPECTED_CI_SHA --context-root . --include path/to/source.go
```

코드 수정이 필요하면 모델에 제한된 수정안을 요청하고, 내부 로직이 원본 파일 해시·경로를 확인한 뒤 적용·포맷·독립 테스트를 수행할 수 있습니다. 실패 작업은 검증을 통과한 뒤에만 확인 처리하며, 불완전한 보고서는 확인 처리할 수 없습니다. 상태 파일은 단일 소비자를 가정합니다. 다른 저장소도 같은 JSON 형식을 제공할 수 있지만, annotations·로그 API 어댑터는 이번 실험에 구현하지 않았습니다. 모델 실험 재현 명령과 실행 횟수는 [영문 안내](../README.md#structured-ci-report-experiment)에 있습니다.

## 개발과 플랫폼 검증

```text
go test ./...
go vet ./...
go run ./scripts/build
```

빌드 스크립트는 셸 없이 Linux/macOS/Windows의 amd64·arm64용 바이너리 6개와 `dist/SHA256SUMS`를 생성합니다. CGO 없이 플랫폼별 디렉터리에 `tools` 또는 `tools.exe`를 저장합니다. CI는 세 OS에서 테스트·검사·현재 플랫폼 빌드를 실행합니다.

테스트는 격리된 HTTP 서버로 GitHub API 동작을 검증하며 실제 저장소에 이슈·PR을 생성하지 않습니다.

## 개인정보·비밀정보 검사

push와 PR의 `gitleaks` CI 작업은 Gitleaks v8.30.1로 전체 Git 변경 이력과 현재 파일을 검사합니다. 기본 토큰·비밀번호·개인키 규칙에 `.gitleaks.toml`의 이메일, 국내 휴대전화 번호, 주민·외국인등록번호 형식 규칙을 추가했습니다. 탐지하면 작업이 실패하며, 탐지된 값은 로그에서 마스킹합니다.

로컬에서도 커밋 전에 같은 검사를 실행할 수 있습니다.

```text
go install github.com/zricethezav/gitleaks/v8@v8.30.1
gitleaks git --config .gitleaks.toml --redact --no-banner --log-opts="--all --full-history" .
gitleaks dir --config .gitleaks.toml --redact --no-banner .
```

예약된 예제 도메인(`example.com/org/net`, `.invalid`, `.test`, `.example`), GitHub noreply 주소, Git SSH 사용자 주소만 이메일 규칙에서 예외로 처리합니다. 테스트 소스 파일 전체를 제외하지 않습니다. `.git` 내부와 생성된 CLI 바이너리·`dist`·Python 바이트코드 캐시는 파일 검사에서 제외합니다.

검사는 파일 내용과 커밋의 변경 내용을 대상으로 하며, 커밋 작성자 이름·이메일은 검사하지 않습니다. 개인 이메일 공개를 피하려면 Git 커밋 이메일을 GitHub noreply 주소로 설정하세요. 이미 기록된 작성자 정보는 이 설정으로 변경되지 않습니다. 이름·주소 같은 자유 형식 정보와 모든 개인정보를 판별하는 검사는 아닙니다.
