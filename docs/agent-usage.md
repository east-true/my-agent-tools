# 에이전트용 빠른 사용법

GitHub 조회·등록은 `tools github`를 사용한다. 아래 명령으로 바로 실행하고, 필요한 옵션을 모를 때만 해당 명령의 `--help`를 읽는다.

세부 옵션·입력·복구 방법은 [명령어별 문서](github/README.md)를 참고한다.

PR 수정 요청을 확인할 때는 `tools github pr reviews --number NUMBER --json`을 사용한다. 제출된 리뷰 이력과 미해결 스레드의 원문·답글·현재/원래 위치를 수집한다. 오래된 미해결 스레드도 포함되므로 `is_outdated`를 확인한다. 해결된 스레드까지 필요하면 `--all`을 추가한다. `review_decision`은 현재 집계이며 과거 `CHANGES_REQUESTED`나 `DISMISSED` 이력을 현재 차단 사유로 단정하지 않는다. `partial`은 `notes`를 확인하고 새 head가 생겼으면 다시 수집한다. [리뷰 수집](github/pr/reviews.md).

CI 재실행이 요청된 작업은 `tools github ci rerun --run RUN_ID --json`으로 실패 job과 의존 job을 다시 실행하고 새 회차의 결과까지 확인한다. 전체 실행은 `--all`, 요청만 보내려면 `--wait=false`, 계획은 `--dry-run`을 사용한다. 새 실패는 `failure`의 자료를 사용한다. `request_attempted`·`rerun_requested`가 있거나 `unknown`이면 원격 실행·회차를 확인한 뒤 다음 조치를 결정한다. 재실행은 원래 커밋을 사용하므로 수정 커밋을 푸시했다면 그 커밋의 새 실행을 확인한다. [재실행과 복구](github/ci-rerun.md).

CI 실패 자료는 `tools github ci failures --run RUN_ID --json`으로 조회한다. 실행 ID는 PR 번호와 다르다. 실패한 job·step·annotation을 수집하고 지원하는 Go 컴파일 오류를 구조화하며, 미지원 오류는 원문 로그를 보존한다. `partial`과 종료 코드 1이면 job별 `notes`에서 누락·권한·상한 사유를 확인한다. 이 명령은 merge·재실행·대기를 수행하지 않는다.

의존성 보안 경고를 확인할 때는 `tools github dependabot list --json`을 사용한다. 기본은 열린 경고 전체이며 `--severity high,critical`, `--ecosystem go`, `--package NAME`으로 필터링한다. 경고 설명과 참고 링크는 `tools github dependabot view --number NUMBER --json`으로 조회한다. `first_patched_version: null`은 패치 정보가 없다는 뜻이다. `403`·`404`는 권한·활성화 여부를 확인하고 빈 목록으로 해석하지 않는다. 조회 결과로 수정 작업을 준비하며 경고 번호를 이슈 번호로 사용하지 않는다.

프로젝트 설정을 처음 저장할 때 `tools github setup`으로 GitHub의 기존 라벨·유형을 가져온다. 기존 설정은 보존하며, 고유 이름은 `--set-label feat=라벨명`·`--set-issue-type fix=유형명`으로 지정한다. 저장 전 확인은 `--dry-run --json`, 현재 목록으로 재계산은 `--refresh`를 사용한다. 생성마다 setup을 반복할 필요는 없다.

1. prefix(`feat`, `fix`, `docs` 등), 영어 제목, Markdown 본문 파일을 준비한다. 본문은 기본적으로 한국어이며, 다른 언어는 `.tools.json`의 `github.body_language: "any"`로 설정한다.
2. `tools github issue create --prefix feat --title "add login" --body-file issue.md --json`을 실행한다. 기존 라벨·유형·템플릿 조회와 검증, 선택한 prefix 추가, 담당자 me 지정, 이슈 생성, 번호 기반 브랜치 생성·Development 연결·로컬 전환까지 자동 처리된다. **계획만 필요하면 같은 명령에 `--dry-run`을 추가하고 반환된 `plan`을 사용한다.** Git 작업 디렉터리 밖에서는 `--repo OWNER/REPO`를 지정한다. 원격 브랜치만 필요하면 `--no-checkout`을 추가한다.
3. 구현·검증 후 커밋·푸시는 Git 명령으로 처리한다.
4. `tools github pr create --prefix feat --title "add login" --body-file pr.md --json`을 실행한다. 현재 브랜치에서 이슈 번호를 읽어 PR에 연결한다. 본문에는 변경 내용과 실제 검증 결과를 포함한다.
5. 머지가 요청된 작업은 `tools github pr merge --number NUMBER --json`을 한 번 실행한다. 검사 대기·머지·실패 원인 조회를 내부에서 처리하므로 별도 검사 호출이 필요 없다. 성공은 `merged`, 실패는 `reasons`의 원인·근거·다음 조치를 확인한다. `timeout`·`unknown`에서 `merge_requested`나 `queued`가 있으면 원격 상태를 확인한 뒤 재실행한다. [옵션과 결과](github/pr/merge.md)를 참고한다.
6. 종료 브랜치를 정리할 때 `tools github branch cleanup --json`으로 후보를 확인한 뒤 `tools github branch cleanup --apply --json`을 실행한다. 최신 PR 머지·클로즈 또는 이슈 종료가 대상이며, 열린 PR·보호/사용 중인 브랜치·로컬 미게시 커밋은 보존한다. 기본은 로컬·원격 모두이며 `--scope local|remote`, `--remote upstream`, `--protect 'develop,release/*'`로 범위를 제한한다. 제외 사유가 필요할 때만 `--include-skipped`를 추가한다. 머지 없이 닫힌 원격 작업도 삭제하므로 실제 정리 요청이 있을 때 `--apply`를 사용한다.

`create`가 필요한 GitHub 조회를 처리하므로 사전에 `context`를 실행할 필요가 없다. 오류나 필요한 정보의 누락이 없으면 반환된 계획·결과를 사용하고 라벨·유형·템플릿을 다시 조회하지 않는다. 목록 자체를 보고 싶을 때만 `context`를 사용한다. 검토가 필요할 때 `--dry-run --json`을 추가하며, 생성 전에 항상 별도로 실행할 필요는 없다.

제목은 영어·숫자·ASCII 기호를 허용하며 대소문자를 보존한다. 소문자 변환은 브랜치 이름에 적용한다. 저장소의 특수한 prefix 매핑은 `.tools.json`에 둔다. 구조화된 본문이 필요하면 `--file JSON`을 사용한다. Git 없이 PR을 생성할 때는 `--repo`와 JSON의 `head`를 지정한다.

`partial` 결과의 URL을 보고 기존 항목을 수정한다. 브랜치 작업만 실패했다면 `tools github issue branch --number 123`으로 이어간다. 이슈를 다시 생성하지 않는다. 생성 요청 결과를 받지 못한 경우에도 원격 상태를 먼저 확인한다. 검증 명령은 기록이며 도구가 실행하지 않는다.
