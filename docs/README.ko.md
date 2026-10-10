# my-agent-tools

**코딩 에이전트와 개발자를 위한, 반복 작업을 한 명령으로 처리하는 자동화 도구.**

[English](../README.md) · [작업 그룹](#사용-가능한-작업-그룹) · [벤치마크](benchmarks/README.md) · [기여 안내](../CONTRIBUTING.md)

`tools`는 반복 작업을 자동화하는 크로스 플랫폼 CLI입니다. 여러 단계를 명령으로 묶어 에이전트와 개발자가 프로젝트마다 재사용하고, 프로젝트별 선호는 설정으로 관리합니다.

작업은 `tools <group> <command>` 형태로 구분하며 GitHub와 파일시스템을 별도 그룹으로 제공합니다.

## 사용 가능한 작업 그룹

| 그룹 | 자동화하는 작업 | 문서 |
|---|---|---|
| `github` | 저장소 설정, 이슈, 연결 브랜치, PR·리뷰, CI 진단·재실행, Dependabot 경고, 종료된 브랜치 정리 | [GitHub 명령어](github/README.md) |
| `fs` | raw 파일·Markdown 섹션·이어 읽기, 증분 조회·배치 수정·JUnit 보고서 판독 | [파일시스템 명령어](fs/README.md) |

## 토큰 사용량 실측

명령별 최신 실측을 모았습니다. 방식별 3회씩 비교했으며, 표는 **캐시 제외 입력+출력 평균**과 캐시 입력을 포함한 총 토큰 변화입니다. 측정일은 한국시간이며 범위·정답/상태 검증은 각 명령 문서에 있습니다.

측정 당시 기본 설정: 2026-10-08: `gpt-6.1-sol` / `high`; 2026-10-10: `gpt-6.1-sol` / `low`; 2026-10-11: `gpt-6.1-sol` / `high`. 서로 다른 설정의 실험을 합산하지 않습니다.

| 명령 | 측정일 | 직접 처리 평균 | tools 평균 | 변화 | 캐시 포함 변화 |
|---|---|---:|---:|---:|---:|
| [`github context`](benchmarks/github/context.md) | 2026-10-08 | 15,573 | 15,324 | −1.6% | −0.8% |
| [`github setup`](benchmarks/github/setup.md) | 2026-10-08 | 17,343 | 15,695 | −9.5% | −36.2% |
| [`github issue create`](benchmarks/github/issue/create.md) | 2026-10-08 | 18,583 | 20,244 | +8.9% | −62.4% |
| [`github issue branch`](benchmarks/github/issue/branch.md) | 2026-10-08 | 15,314 | 15,626 | +2.0% | −40.8% |
| [`github pr create`](benchmarks/github/pr/create.md) | 2026-10-11 | 18,491 | 15,713 | −15.0% | −59.0% |
| [`github pr reviews`](benchmarks/github/pr/reviews.md) | 2026-10-11 | 13,978 | 18,389 | +31.6% | +0.6% |
| [`github pr inspect`](benchmarks/github/pr/inspect.md) | 2026-10-10 | 19,458 | 17,753 | −8.8% | −14.3% |
| [`github pr delta`](benchmarks/github/pr/delta.md) | 2026-10-08 | 16,851 | 15,314 | −9.1% | −51.7% |
| [`github pr submit`](benchmarks/github/pr/submit.md) | 2026-10-08 | 19,974 | 15,777 | −21.0% | −66.0% |
| [`github pr merge`](benchmarks/github/pr/merge.md) | 2026-10-08 | 20,302 | 11,468 | −43.5% | −76.3% |
| [`github ci failures`](benchmarks/github/ci/failures.md) | 2026-10-08 | 21,616 | 16,019 | −25.9% | −50.3% |
| [`github ci rerun`](benchmarks/github/ci/rerun.md) | 2026-10-08 | 18,078 | 15,673 | −13.3% | −61.7% |
| [`github dependabot list`](benchmarks/github/dependabot/list.md) | 2026-10-08 | 16,036 | 15,503 | −3.3% | −2.2% |
| [`github dependabot view`](benchmarks/github/dependabot/view.md) | 2026-10-08 | 11,243 | 15,175 | +35.0% | −0.7% |
| [`github branch cleanup --apply`](benchmarks/github/branch/cleanup.md) | 2026-10-08 | 30,151 | 21,365 | −29.1% | −54.9% |

고정 합성 자료·소표본 결과이며 캐시 적중 차이가 남습니다. 독립 1회 사용·다른 작업 후 복귀의 동시 개선이나 일반적인 절감률·요금 절감을 보장하지 않습니다. [공통 조건·한계·원본·재현](benchmarks/github/README.md).

### 파일시스템 실측

파일시스템 실험별 기본 설정: 2026-10-10: `gpt-6.1-sol` / `low`; 2026-10-11: `gpt-6.1-sol` / `high`. 보통 방식별 3회이며 JUnit 일반 판독은 2쌍입니다. 캐시 제외 평균과 캐시 포함 총 토큰 변화를 함께 표시합니다.

| 명령 | 직접 평균 | tools 평균 | 캐시 제외 변화 | 캐시 포함 변화 | 정답·상태 | 동시 개선 판단 |
|---|---:|---:|---:|---:|---:|---|
| [`fs inspect: Markdown`](benchmarks/fs/inspect.md) | 21,294 | 16,324 | −23.3% | −42.0% | 6/6 | 표본에서 AND 충족 |
| [`fs inspect: raw file`](benchmarks/fs/inspect.md) | 16,459 | 15,601 | −5.2% | −2.3% | 6/6 | 표본에서 AND 충족 |
| [`fs delta`](benchmarks/fs/delta.md) | 18,026 | 16,674 | −7.5% | −12.9% | 6/6 | 표본에서 AND 충족 |
| [`fs apply: 오류 진단`](benchmarks/fs/apply.md) | 17,178 | 15,768 | −8.2% | −50.7% | 6/6 | 표본에서 AND 충족 |
| [`fs apply: 공통 치환`](benchmarks/fs/apply.md) | 18,583 | 17,300 | −6.9% | −1.6% | 6/6 | 표본에서 AND 충족 |
| [`fs test-results`](benchmarks/fs/test-results.md) | 21,844 | 25,794 | +18.1% | −11.5% | 4/4 | 조건부·캐시 지표 상충 |

독립 1회 사용·다른 실제 작업 후 재사용은 별도 검증하지 않았습니다. 캐시를 통제하지 않아 최초 표본도 캐시 없는 실행이 아닙니다. AND 판정은 측정 표본의 평균에 한정합니다.

[조건·한계·원본·재현](benchmarks/fs/README.md).

리뷰·PR 복귀·apply 두 시나리오의 최신 수치는 입력 작성·실제 완료 검증을 포함합니다. [범위·캐시 한계·원본](benchmarks/actionable/README.md).

사용 기록에서 찾은 최신 작업의 토큰·시간·실제 CLI/API 수와 조건부 결과는 [별도 완료 비교](benchmarks/usage/README.md)에 있습니다. JUnit 표는 일반 판독 2쌍이며 XML 오류 1쌍은 별도 공개합니다.

최신 변경 작업 비교는 리뷰 원문·복귀, 큰 보고서 복구, 실제 설정 저장 검증과 저장된 CI 원문 선택을 다룹니다. 직접 처리와 tools의 같은 완료 작업에서 입력 작성·복구·최종 검증을 포함하고 불리한 실행과 캐시 상충도 보존합니다. [결과·원본](benchmarks/completion/README.md).

## 설치

소스 저장소에서 **Go 1.26 이상**으로 빌드합니다.

```sh
go build -o tools ./cmd/tools
```

Windows는 `go build -o tools.exe ./cmd/tools`를 사용합니다. 바이너리 디렉터리를 PATH에 추가하세요. Linux·macOS·Windows에서 실행하며 Go 런타임은 필요하지 않습니다.

## 빠른 시작

사용 가능한 작업과 명령을 확인합니다.

```sh
tools --help
```

### GitHub

인증은 `GH_TOKEN` → `GITHUB_TOKEN` → 기존 `gh auth login` 순으로 사용합니다. 환경변수로 토큰을 제공하면 `gh` 설치는 선택 사항입니다. 이 그룹은 현재 `github.com`을 지원하며 로컬 브랜치 작업에는 Git이 필요합니다.

대상 프로젝트의 Git 작업 디렉터리에서 실행합니다. 이슈·PR 설명을 UTF-8 Markdown 파일 `issue.md`, `pr.md`로 준비하세요. 본문 언어의 기본값은 한국어입니다.

```sh
# 기존 GitHub 라벨·유형을 가져와 프로젝트 설정 저장
tools github setup

# 이슈 생성, 담당자 지정, 연결 브랜치·별도 워크트리 생성
tools github issue create --prefix fix --title "handle duplicate requests" --body-file issue.md --json

# 반환된 branch.worktree_path로 이동
# 구현·검증 후 Git으로 커밋·푸시하고 PR 생성
tools github pr create --prefix fix --title "handle duplicate requests" --body-file pr.md --json

# 제출된 리뷰 본문·미해결 대화·코드 위치 수집
tools github pr reviews --number 123 --json

# 한 번 호출해 검사 완료 대기·조건 충족 시 머지·실패 원인 반환
tools github pr merge --number 123 --json
```

생성 명령이 필요한 목록을 조회하므로 `context`는 선택 사항입니다. 생성 계획만 확인하려면 `--dry-run --json`, Git 밖에서는 `--repo OWNER/REPO`를 추가합니다. 다른 언어는 `tools github setup --body-language any`로 허용하고, 라벨 지정 등은 [setup 문서](github/setup.md)를 참고하세요.

워크트리 기본 경로는 `<주 작업 폴더>.worktrees/<브랜치>`이며 현재 브랜치와 작업 파일은 유지합니다. 같은 브랜치의 워크트리는 재사용하고, `--no-checkout`으로 로컬 생성을 생략합니다.

반복되는 PR 작업을 조합 명령으로 처리하고 같은 자료의 재출력을 줄입니다.

```sh
# 기존 커밋 푸시·PR 재사용/생성·검사 조회
tools github pr submit --prefix fix --title "handle duplicate requests" --body-file pr.md --json
# PR 상태·리뷰·검사·실패 자료. 같은 상태 파일로 반복하면 변경분만 반환
tools github pr inspect --number 123 --state-file .tools/state/pr-123.json --json
# 머지 후 해당 PR의 종료 브랜치만 정리
tools github pr merge --number 123 --json
```

통합 조회·제출·머지는 바이트와 토큰이 모두 줄어들 때 원문을 파일과 SHA-256으로 보존하고 간결하게 반환합니다. 리뷰·CI는 `--compact`로 선택합니다. 머지는 실제 성공을 확인한 뒤 해당 PR 정리까지 이어지며 `--cleanup=false`로 생략할 수 있습니다. [리뷰 출력 토큰 측정](benchmarks/github/pr/reviews.md)은 두 인코딩에 한정합니다. 파일 변경은 `pr delta --number 123 --since FULL_SHA`로 조회하며 patch는 `--include-patch`를 줄 때만 포함합니다. [최신 증분 조회 측정](benchmarks/github/pr/delta.md)은 기준 SHA 검증을 포함한 기본 patch 생략 시나리오입니다.

실패한 CI job을 재실행하고 새 회차의 완료까지 기다립니다. 다시 실패하면 실패 자료를 함께 반환합니다.

```sh
tools github ci rerun --run 123456789 --json
```

전체 workflow는 `--all`, 요청 수락 후 반환은 `--wait=false`, 미리보기는 `--dry-run`을 사용합니다. 재실행은 원래 커밋을 사용하므로 수정 커밋을 푸시했다면 새 실행을 확인하세요. [옵션과 복구](github/ci/rerun.md).

의존성 보안 경고와 패치 정보를 조회합니다.

```sh
tools github dependabot list --severity high,critical --json
tools github dependabot view --number 7 --json
```

목록은 기본적으로 열린 경고를 모든 페이지에서 가져옵니다. Fine-grained 토큰에는 Dependabot alerts 읽기 권한이 필요합니다. [목록 옵션과 결과 필드](github/dependabot/list.md) · [경고 상세](github/dependabot/view.md).

PR이나 이슈 종료 후 주 작업 폴더로 돌아와 정리 대상을 확인합니다.

```sh
tools github branch cleanup --json
# 후보 확인 후 실제 삭제
tools github branch cleanup --apply --json
```

종료된 브랜치의 깨끗한 연결 워크트리는 함께 제거합니다. 주 작업 폴더·실행 중인 워크트리, 잠겼거나 변경 파일이 있는 워크트리, 보호 브랜치, 열린 PR 작업, 로컬 미게시 커밋은 보존합니다. 머지 없이 닫힌 원격 작업도 삭제 대상이므로 미리보기를 확인하세요. [정리 조건과 옵션](github/branch/cleanup.md).
### 파일시스템 작업

```sh
tools fs inspect --root . --include '**/*.go' --pattern cache --pattern retry --context 2 --json
tools fs inspect --request examples/fs/read-requests.json --json
# 수정에 사용할 원문 조각과 정확한 줄바꿈·해시 조회
tools fs inspect --request examples/fs/read-requests.json --raw --hash --json
tools fs delta --root . --include '**/*.go' --state-file .tools/state/fs/go.json --include-content --json
# 실제 두 번 관찰하고 기준을 보존하며 동일 보고서는 한 번만 출력
tools fs delta --include '**/*.go' --state-file .tools/state/fs/go.json --include-content --peek --comparisons 2 --json
tools fs apply --root . --plan edits.json --json
tools fs apply --root . --plan edits.json --apply --report-changes --json
# 이미 승인한 수정 조건을 계획 생성부터 재검증까지 처리
tools fs apply --spec replacements.json --save-plan validated-plan.json --apply --report-changes --json
# 해당 치환의 횟수 수정을 지시한 경우 진단·복구까지 통합
tools fs apply --spec replacements.json --recount 1:1 --save-plan recounted-plan.json --apply --report-changes --json
```

Git·GitHub 인증 없이 사용합니다. `apply`는 기본 미리보기이며 계획 전체의 SHA·치환 횟수를 먼저 확인하고 실제 반영한 파일을 다시 읽어 검증합니다. 조회 누락·상한 초과 시 `partial`로 반환하고 증분 기준은 갱신하지 않습니다. [옵션·계획 형식·반영 범위](fs/README.md).

## 문서

- [GitHub 명령어 안내](github/README.md) — 명령, 옵션, 설정, 실패 복구.
- [GitHub 에이전트용 빠른 사용법](agent-usage.md) — 코딩 에이전트에 전달할 짧은 작업 흐름.
- [입력·설정 예제](../examples/github) — 이슈·PR JSON, Markdown, `.tools.json`.
- [벤치마크](benchmarks/README.md) — 명령별 최신 측정, 원본 자료, 재현 방법.
- [전체 명령 점검](command-audit.md) — 이전 18개 점검과 현재 19개 명령의 추가 기능 검증 근거.
- [개발·개인정보 검사](development.md) · [CI 보고서 실험](ci-reports.md).

명령 목록은 `tools --help`, 세부 옵션은 각 명령의 `--help`에서 확인합니다.

## 기여

새 작업 그룹, 다른 프로젝트의 사용 사례, 버그 제보, 코드 기여를 환영합니다. [기여 안내](../CONTRIBUTING.md), [보안 정책](../SECURITY.md), [저장소 정책](repository.md)을 참고하세요.

[MIT 라이선스](../LICENSE)로 배포합니다.
