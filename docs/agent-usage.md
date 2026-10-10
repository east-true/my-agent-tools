# 에이전트용 빠른 사용법

GitHub에서 여러 조회·검증·반영을 이어야 하면 `tools github`를 사용한다. 필요한 옵션을 모를 때만 해당 명령의 `--help`를 읽는다. 이미 알고 있는 ID의 단일 필드 조회처럼 직접 `gh api`로 작업과 검증을 짧게 끝낼 수 있으면 직접 처리도 선택한다. 명령 사용 자체를 필수 절차로 추가하지 않는다.

세부 옵션·입력·복구 방법은 [명령어별 문서](github/README.md)를 참고한다.

완료된 CI 실패 자료는 명령 간 공통 캐시를 사용한다. 다른 명령으로 같은 실행을 확인할 때도 메타데이터를 재검증하며, 자료가 이미 포함돼 있으면 별도로 다시 조회하지 않는다. `ci rerun`이 시간 초과·대기 생략으로 끝났다면 `tools github ci rerun --run RUN_ID --resume --json`으로 저장된 회차의 관찰만 재개한다. 해결되지 않은 저장 요청에 새 재실행을 보내지 않는다. [조회 재사용](github/README.md#조회-재사용과-호출-제한) · [재개 옵션](github/ci/rerun.md).

PR의 전체 현재 상태가 필요하면 `tools github pr inspect --number NUMBER --json`을 먼저 사용한다. 리뷰·검사·Actions 실행 ID·해당 회차의 실패 자료를 함께 반환하므로 각각 재조회하지 않는다. 필요한 범위만 조회할 때는 `--sections checks,reviews,failures`에서 선택한다. 진행 중인 검사를 기다리려면 `--wait`, 반복 조회에는 `--state-file .tools/state/pr-NUMBER.json`을 사용한다. `unchanged`는 새 관찰이 없다는 뜻이며 작업 완료가 아니다. `attention_required`·`pr_status`·현재 `outstanding`을 확인한다. 부분 조회는 기준을 갱신하지 않는다. [통합 조회](github/pr/inspect.md).

이미 커밋한 변경의 푸시와 PR 제출이 함께 요청되면 `tools github pr submit --prefix fix --title "handle duplicate requests" --body-file pr.md --json`을 사용한다. 푸시 전 base·정책을 검증하고 같은 브랜치의 열린 PR은 재사용하며 기본으로 검사 결과까지 기다린다. 별도 푸시·PR 생성·검사 조회를 반복하지 않는다. `tools github pr merge --number NUMBER --json`은 머지 확인 후 해당 브랜치 정리까지 기본으로 이어진다. 정리를 생략할 때만 `--cleanup=false`를 사용한다. 다른 종료 브랜치는 정리하지 않는다. [제출](github/pr/submit.md) · [머지와 정리](github/pr/merge.md).

리뷰·CI 결과에 `--compact`를 사용할 수 있다. 리뷰 본문·diff·진단 메시지는 그대로 유지하지만 CI 로그 등의 상세 원문이 필요하면 처음부터 `--compact=false --json`을 사용해 원문 파일 재조회를 피한다. 필요가 뒤늦게 생기면 `evidence_file`의 원문과 SHA를 확인한다. 지정한 인코딩에서 파일 참조까지 포함한 출력 토큰 수가 줄어들 때만 축소하지만, 원문 재조회까지 포함한 작업 전체 절감은 별도 판단한다. 원문은 기본 30일·최근 200개를 보관하므로 장기 참조는 별도로 저장한다. PR 파일 변경 목록만 필요하면 `tools github pr delta --number NUMBER --since BASELINE_SHA --json`을 사용하고 patch가 필요할 때는 처음부터 `--include-patch`를 추가한다. [최신 측정](benchmarks/github/pr/delta.md)은 기준 SHA 검증을 포함한 기본 조회 시나리오에 한정되므로 모든 단순 조회의 절감률로 해석하지 않는다. [간결한 출력](github/README.md#간결한-출력) · [증분 조회](github/pr/delta.md).

다른 작업 후 복귀하면 PR 증분 결과의 `outstanding`에 있는 현재 미해결 사유·검사·실패 자료·스레드 원문과 위치를 사용한다. `unchanged`만 보고 완료로 판정하지 않는다. 전체 리뷰 이력·성공 검사·실행 목록도 필요할 때는 처음부터 `--full`을 사용한다. 리뷰 본문·diff·진단 메시지는 compact에서도 그대로 유지하며 긴 CI 로그 등은 여전히 생략될 수 있으므로 표시를 확인한다. CI 자료 캐시는 최신 실행 ID·회차·SHA 등을 확인한 뒤 자료 수집을 재사용하는 로컬 기능이며 모델 입력 캐시와 별개다. 독립 1회 사용과 복귀 시 사용에서 작업 효율과 전체 토큰이 함께 개선되는지는 [전체 명령 점검](command-audit.md)을 참고한다.

PR 수정 요청을 확인할 때는 `tools github pr reviews --number NUMBER --json`을 사용한다. 제출된 리뷰 이력과 미해결 스레드의 원문·답글·현재/원래 위치를 수집한다. 오래된 미해결 스레드도 포함되므로 `is_outdated`를 확인한다. 해결된 스레드까지 필요하면 `--all`을 추가한다. `review_decision`은 현재 집계이며 과거 `CHANGES_REQUESTED`나 `DISMISSED` 이력을 현재 차단 사유로 단정하지 않는다. `partial`은 `notes`를 확인하고 새 head가 생겼으면 다시 수집한다. [리뷰 수집](github/pr/reviews.md).

`tools`는 셸 실행 파일이며 `github`·`fs`는 그 하위 명령이다. 일반 PR 대화도 필요하면 `--conversation`을 추가한다. `head_sha`와 `head_verified: true`는 수집 끝에서 확인한 head 근거이므로 같은 조회의 head를 얻으려고 전체 PR 조회를 추가하지 않는다. 수정 후 새 head 확인이 지시됐다면 그 시점에 다시 읽는다.

같은 작업에서 리뷰 원문을 다시 처리해야 하면 `--save-result FILE`로 작업 폴더의 새 파일에 저장한다. 저장된 일반 JSON은 Python `json.load`로 읽고 repo·number·head_sha·complete와 반환된 파일 SHA를 확인한다. 매번 전체 리뷰 수집이나 filesystem inspect로 JSON을 다시 포장할 필요가 없다. 이후 원격 리뷰 변경까지 필요한 작업에는 현재 상태 조회를 사용한다.

CI 재실행이 요청된 작업은 `tools github ci rerun --run RUN_ID --json`으로 실패 job과 의존 job을 다시 실행하고 새 회차의 결과까지 확인한다. 전체 실행은 `--all`, 요청만 보내려면 `--wait=false`, 계획은 `--dry-run`을 사용한다. 새 실패는 `failure`의 자료를 사용한다. `request_attempted`·`rerun_requested`가 있거나 `unknown`이면 원격 실행·회차를 확인한 뒤 다음 조치를 결정한다. 재실행은 원래 커밋을 사용하므로 수정 커밋을 푸시했다면 그 커밋의 새 실행을 확인한다. [재실행과 복구](github/ci/rerun.md).

CI 실패 자료만 필요하면 `tools github ci failures --run RUN_ID --json`으로 조회한다. 실행 ID는 PR 번호와 다르다. 실패한 job·step·annotation과 지원하는 Go 컴파일 오류·Go/pytest 실패를 수집하며, 미지원 오류는 원문 로그를 보존한다. `partial`과 종료 코드 1이면 job별 `notes`에서 누락·권한·상한 사유를 확인한다. 이 명령은 merge·재실행·대기를 수행하지 않는다. `pr merge`·`pr inspect`·`ci rerun`도 같은 수집기를 사용하므로 해당 명령에 실패 자료가 이미 포함돼 있으면 별도로 재조회하지 않는다. 머지는 기본 간결한 출력과 원문 파일 보존을 사용한다. `--compact=false --json`은 전체 자료를 반환한다. 머지 결과의 `failures`는 회차별 수집 자료이고 `reasons[].details`는 제한된 읽기용 근거다.

의존성 보안 경고를 확인할 때는 `tools github dependabot list --json`을 사용한다. 기본은 열린 경고 전체이며 `--severity high,critical`, `--ecosystem go`, `--package NAME`으로 필터링한다. 경고 설명과 참고 링크는 `tools github dependabot view --number NUMBER --json`으로 조회한다. `first_patched_version: null`은 패치 정보가 없다는 뜻이다. `403`·`404`는 권한·활성화 여부를 확인하고 빈 목록으로 해석하지 않는다. 조회 결과로 수정 작업을 준비하며 경고 번호를 이슈 번호로 사용하지 않는다.

프로젝트 설정을 처음 저장할 때 `tools github setup`으로 GitHub의 기존 라벨·유형을 가져온다. 기존 설정은 보존하며, 고유 이름은 `--set-label feat=라벨명`·`--set-issue-type fix=유형명`으로 지정한다. 저장 전 확인은 `--dry-run --json`, 현재 목록으로 재계산은 `--refresh`를 사용한다. 생성마다 setup을 반복할 필요는 없다.

1. prefix(`feat`, `fix`, `docs` 등), 영어 제목, Markdown 본문 파일을 준비한다. 본문은 기본적으로 한국어이며, 다른 언어는 `.tools.json`의 `github.body_language: "any"`로 설정한다.
2. `tools github issue create --prefix feat --title "add login" --body-file issue.md --json`을 실행한다. 기존 라벨·유형·템플릿 조회와 검증, 선택한 prefix 추가, 담당자 me 지정, 이슈 생성, 번호 기반 브랜치 생성·Development 연결·별도 워크트리 생성까지 자동 처리된다. **계획만 필요하면 같은 명령에 `--dry-run`을 추가하고 반환된 `plan`을 사용한다.** Git 작업 디렉터리 밖에서는 `--repo OWNER/REPO`를 지정한다. 원격 브랜치만 필요하면 `--no-checkout`을 추가한다.
3. 반환된 `branch.worktree_path`로 이동해 구현·검증한다. 기본 경로는 `<주 작업 폴더>.worktrees/<브랜치>`이며 기존 워크트리는 재사용한다. 현재 폴더의 브랜치·작업 파일은 유지되며, 커밋·푸시는 Git 명령으로 처리한다.
4. `tools github pr create --prefix feat --title "add login" --body-file pr.md --json`을 실행한다. 현재 브랜치에서 이슈 번호를 읽어 PR에 연결한다. 본문에는 변경 내용과 실제 검증 결과를 포함한다.
5. 머지가 요청된 작업은 `tools github pr merge --number NUMBER --json`을 한 번 실행한다. 검사 대기·머지·해당 브랜치 정리·실패 원인 조회를 내부에서 처리한다. 성공은 `merged`, 머지 후 정리 실패·보존은 `partial`과 `merged: true`, `cleanup.actions`를 확인한다. 검사·머지 실패는 `reasons`의 원인·근거·다음 조치를 확인한다. `timeout`·`unknown`에서 `merge_requested`나 `queued`가 있으면 원격 상태를 확인한 뒤 재실행한다. [옵션과 결과](github/pr/merge.md)를 참고한다.
6. 종료 브랜치를 정리할 때 주 작업 폴더로 돌아와 `tools github branch cleanup --json`으로 후보를 확인한 뒤 `tools github branch cleanup --apply --json`을 실행한다. 최신 PR 머지·클로즈 또는 이슈 종료가 대상이며 깨끗한 연결 워크트리를 함께 제거한다. 주 작업 폴더·실행 중인 워크트리, 잠겼거나 수정·미추적·무시 파일이 있는 워크트리, 열린 PR·보호 브랜치·로컬 미게시 커밋은 보존한다. 기본은 로컬·원격 모두이며 `--scope local|remote`, `--remote upstream`, `--protect 'develop,release/*'`로 범위를 제한한다. `remote` 범위는 워크트리를 제거하지 않는다. 제외 사유가 필요할 때만 `--include-skipped`를 추가한다. 머지 없이 닫힌 원격 작업도 삭제하므로 실제 정리 요청이 있을 때 `--apply`를 사용한다.

`create`가 필요한 GitHub 조회를 처리하므로 사전에 `context`를 실행할 필요가 없다. 오류나 필요한 정보의 누락이 없으면 반환된 계획·결과를 사용하고 라벨·유형·템플릿을 다시 조회하지 않는다. 목록 자체를 보고 싶을 때만 `context`를 사용한다. 검토가 필요할 때 `--dry-run --json`을 추가하며, 생성 전에 항상 별도로 실행할 필요는 없다.

제목은 영어·숫자·ASCII 기호를 허용하며 대소문자를 보존한다. 소문자 변환은 브랜치 이름에 적용한다. 저장소의 특수한 prefix 매핑은 `.tools.json`에 둔다. 구조화된 본문이 필요하면 `--file JSON`을 사용한다. Git 없이 PR을 생성할 때는 `--repo`와 JSON의 `head`를 지정한다.

`partial` 결과의 URL을 보고 기존 항목을 수정한다. 브랜치 작업만 실패했다면 `tools github issue branch --number 123`으로 이어간다. 이슈를 다시 생성하지 않는다. 생성 요청 결과를 받지 못한 경우에도 원격 상태를 먼저 확인한다. 검증 명령은 기록이며 도구가 실행하지 않는다.
