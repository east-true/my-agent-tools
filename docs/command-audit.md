# 전체 명령 점검

현재 19개 명령의 입력·출력·후속 작업 점검과 신규 후보는 [전체 명령 개선안](command-improvements.md)에 있다. 이 점검은 도움말 19개·출력 관찰 3개·구현/기존 실행 기록을 사용했으며 모델 토큰을 새로 측정하지 않았다. 아래 과거 18개 기능 점검과 범위를 구분한다.

2026-10-11에는 [사용 기록 기반 개선](usage-based-improvements.md)으로 `fs test-results`를 추가해 현재 명령은 19개입니다. 아래 18개 점검은 2026-10-10의 범위이며 새 기능의 실행 검증·토큰 비교는 [별도 자료](benchmarks/usage/README.md)에 보관합니다. 빌드와 실제 OS 실행 검증을 구분합니다.

2026-10-10 기준 GitHub 15개·파일시스템 3개를 다시 점검했습니다. 기본 사용 조건은 **독립 1회 사용**이며 목표는 **작업 효율 향상 AND 작업 전체 토큰 절감**입니다. 모델 입력 캐시 적중이나 같은 명령의 연속 반복을 전제로 삼지 않습니다.

개선 후에도 18개 대표 시나리오의 실제 CLI 실행·정답·최종 상태 검증이 모두 통과했습니다. 이 전체 기능 점검의 모델 호출은 **0회**입니다. 별도로 변경 동작 네 가지의 [완료 작업·토큰 비교](benchmarks/actionable/README.md)를 24회 수행했습니다. 아래 표는 기능 점검이며 각 시나리오의 기준 생성·파일 수정·재사용 검증을 포함하므로 18번의 단일 CLI 호출로 해석하지 않습니다.

| 명령 | 독립 사용에서 필요한 입력과 완료 범위 | 다른 작업 후 복귀·추가 호출을 피하는 방법 |
|---|---|---|
| [github context](github/context.md) | 저장소 목록·정책 자체가 필요할 때 조회 | 생성 명령 앞에 매번 붙이지 않음. 카탈로그를 새로 조회하므로 이전 결과에 의존하지 않음 |
| [github setup](github/setup.md) | 최초 설정 또는 명시적 갱신. 기존 라벨·유형으로 설정 저장 | 생성마다 반복하지 않음. 기존 설정 보존과 `--refresh` 재계산을 구분 |
| [github issue create](github/issue/create.md) | 제목·본문을 받아 목록 검증→담당자→생성→브랜치·Development 연결·워크트리까지 수행 | `context`·`setup`·미리보기는 필수 선행 단계가 아님. `partial`이면 기존 이슈를 사용해 브랜치 작업만 복구 |
| [github issue branch](github/issue/branch.md) | 기존 이슈 번호로 연결·브랜치·워크트리 구성 | 같은 이슈로 재실행하면 연결·브랜치·워크트리를 확인해 재사용. 이슈 재생성 불필요 |
| [github pr create](github/pr/create.md) | 제목·본문·게시된 head로 목록·이슈·비교 검증 후 생성 | 이미 게시된 커밋의 PR 생성용. 생성 결과가 불명확하면 원격 확인부터 수행 |
| [github pr reviews](github/pr/reviews.md) | PR 번호로 리뷰 이력·미해결 스레드 원문·위치 수집 | compact도 본문·diff를 그대로 유지. 생략된 중복 메타데이터 전체가 필요할 때 전체 출력 |
| [github pr inspect](github/pr/inspect.md) | PR 번호로 현재 상태·리뷰·검사·실행·해당 회차 실패 자료 수집 | 증분의 현재 `outstanding`으로 복귀 후 미해결 작업 처리. 이력·성공 검사까지 필요할 때 `--full`. `unchanged`만으로 완료 판정하지 않음 |
| [github pr delta](github/pr/delta.md) | PR 번호와 알고 있는 전체 기준 SHA로 변경 파일 비교 | 수정용 patch가 필요하면 처음부터 `--include-patch`. 잘못된 기준·재작성된 이력은 `needs_refresh`; 기준 확보 비용도 집계 |
| [github pr submit](github/pr/submit.md) | 이미 커밋한 변경을 푸시→열린 PR 재사용/생성→검사 관찰 | push·create·inspect를 따로 반복하지 않음. 제출은 자동 커밋이나 머지를 수행하지 않음 |
| [github pr merge](github/pr/merge.md) | PR 번호로 검사 대기→SHA에 맞는 머지→실제 머지 확인→해당 브랜치 정리 | 정리 명령을 다시 붙이지 않음. `merged: true`인 부분 실패는 머지와 정리 결과를 구분. 요청 수락 후 timeout은 원격 상태 확인 |
| [github ci failures](github/ci/failures.md) | 실행 ID로 해당 회차의 job·annotation·로그·진단 수집 | 통합 조회·머지·재실행 응답에 이미 포함된 자료를 재조회하지 않음. 원문 수정 작업에는 기본 전체 출력 사용 |
| [github ci rerun](github/ci/rerun.md) | 실행 ID로 재실행 요청→다음 회차 관찰→실패 자료 반환 | 체크포인트는 중복 요청 방지용. 미해결 요청에는 `--resume`으로 관찰만 재개. 이전 실행 결과로 완료 판정하지 않음 |
| [github dependabot list](github/dependabot/list.md) | 필요한 필터로 모든 페이지의 경고·영향 버전 목록 수집 | 설명이 필요한 경고만 view. 전체 목록이 불필요하면 최초 요청부터 필터 지정 |
| [github dependabot view](github/dependabot/view.md) | 알고 있는 경고 번호의 설명·영향 버전·참고 링크 조회 | list 선행 불필요. 알려진 필드 한 개만 필요하면 직접 `gh api` 조회도 선택 가능 |
| [github branch cleanup](github/branch/cleanup.md) | 기본 후보 조회; 정리 요청 시 `--apply`로 최신 상태 재검증·정리 | 정리가 이미 지시되었다면 별도 미리보기를 의무화하지 않음. 열린 PR·미게시 커밋·작업 파일·보호 브랜치 확인 유지 |
| [fs inspect](fs/inspect.md) | 파일별 범위·패턴을 한 요청으로 읽음. 편집에는 `--raw --hash`로 원문·SHA 제공 | 기본 상한으로 시작하고 `next_cursor`가 있을 때만 이어 읽음. 복귀 후 낡은 커서는 사용하지 않음 |
| [fs delta](fs/delta.md) | 저장된 동일 루트·선택 범위의 기준과 현재 파일을 실제 해시로 비교 | 최초 기준 생성은 과거 변경을 복원하지 않음. 한 번의 관찰에 `--comparisons` 반복을 추가하지 않음. 기준이 없으면 현재 조회는 inspect |
| [fs apply](fs/apply.md) | spec/plan으로 전체 검증→반영→실제 파일 재검증. `--spec --save-plan --apply --report-changes`로 연결 가능 | 공통 치환은 groups 한 번 입력, 독립 파일 오류는 diagnostics로 함께 확인. 캐시·연속 실행 불필요. 오래된 plan은 SHA 검증, 새 spec은 현재 파일을 검증 |

짧은 출력만으로는 동시 개선을 판정할 수 없습니다. compact는 출력의 바이트·인코딩 토큰이 모두 줄어드는지 확인하지만 원문 파일을 다시 읽어야 하면 추가 호출과 토큰을 포함해야 합니다. 작업에 필요한 원문을 처음부터 받는 옵션을 선택하도록 [GitHub 사용 안내](agent-usage.md)와 [파일시스템 사용 안내](fs/agent-usage.md)를 갱신했습니다. 단일 필드 조회·단순 검색·치환은 직접 처리도 허용합니다.

로컬 CI 자료 캐시는 실행 ID·회차·SHA·완전성 등을 확인하고 API 자료 수집을 재사용합니다. PR 상태 파일과 FS 기준 파일은 비교 기준이고 CI 체크포인트는 중복 변경 요청 방지용입니다. 어느 것도 모델 입력 캐시 적중을 보장하지 않습니다. `fs apply`는 입력 spec/plan과 현재 파일로 동작하며 재사용 캐시를 요구하지 않습니다.

최신 [완료 작업 비교](benchmarks/actionable/README.md)는 일괄 오류 진단·공통 치환·PR 복귀의 표본 평균에서 AND를 충족했습니다. 리뷰 반영은 토큰·셸 실행 수는 줄었지만 평균 시간이 늘어 부분 개선입니다. 입력 파일 이름을 불필요하게 제한한 검증기 오류 1건은 원래 실패를 보존하고 같은 기록·파일 SHA로 재검증했으며 모델 실행을 교체하지 않았습니다. 모델 입력 캐시는 통제하지 못했고 복귀는 합성 fixture이므로 18개 명령 모두의 독립 사용에서 안정적인 AND가 성립한다고 주장하지 않습니다. 과거 불리한 결과도 원본에 유지하며 기능을 금지하거나 삭제할 근거로 사용하지 않습니다.

기능 검증은 Linux에서 실제 Git·gh·CLI, 임시 bare 원격, 로컬 GraphQL/REST 서버와 파일 fixture로 수행했습니다. GitHub 생성·연결·푸시·머지·정리의 실제 임시 상태, 조회의 비변경성, 파일의 최종 바이트·기존 권한·해시·기준 보존·계획 저장을 확인했습니다. 리뷰 compact, CI 자료 재사용, 이슈 브랜치 재사용, delta 두 번 관찰과 apply의 명시적 재계수 복구도 포함합니다. 모든 옵션 조합·실제 GitHub 네트워크 성능·macOS/Windows 런타임을 포괄하지 않습니다. 재실행·변경된 SHA·부분 수집·기준 충돌·낡은 커서 등 오류/복귀 경로는 기존 Go 테스트로 별도 확인합니다.

[점검 요약 JSON](benchmarks/command-audit.json)은 소스·실행기·CLI SHA-256, 실행 인자, 정규화된 결과, 상태 검증 항목을 담습니다. 모델 없는 기능 점검을 다음처럼 재현할 수 있습니다. 두 명령은 토큰 측정을 시작하지 않습니다.

```sh
python3 -m pip install --require-hashes -r scripts/benchmarks/requirements.txt
python3 scripts/benchmarks/run_github_study.py --root /tmp/github-command-audit
python3 scripts/benchmarks/run_fs_benchmark.py --root /tmp/fs-command-audit --recount-workflows
go test ./... -count=1
```

유료 모델 재측정이 필요하면 독립 1회·다른 작업 후 복귀·연속 반복을 각각 분리하고 기준 준비·원문 재조회·복구·검증까지 같은 완료 범위로 비교해야 합니다. 전체 입력+출력, 캐시 제외 입력+출력, 정확성, 호출 수, 완료 시간을 함께 보고하며 통제되지 않은 첫 실행을 캐시 없는 최초 실행이라고 표시하지 않습니다.
