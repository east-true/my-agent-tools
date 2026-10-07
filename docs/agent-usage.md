# 에이전트용 빠른 사용법

GitHub 조회·등록은 `tools github`를 사용한다. 아래 명령으로 바로 실행하고, 필요한 옵션을 모를 때만 해당 명령의 `--help`를 읽는다.

1. prefix(`feat`, `fix`, `docs` 등), 영어 제목, Markdown 본문 파일을 준비한다. 본문은 기본적으로 한국어이며, 다른 언어는 `.tools.json`의 `github.body_language: "any"`로 설정한다.
2. `tools github issue create --prefix feat --title "add login" --body-file issue.md --json`을 실행한다. 기존 라벨·유형·템플릿 조회와 검증, 선택한 prefix 추가, 담당자 me 지정, 이슈 생성, 번호 기반 브랜치 생성·Development 연결·로컬 전환까지 자동 처리된다. **계획만 필요하면 같은 명령에 `--dry-run`을 추가하고 반환된 `plan`을 사용한다.** Git 작업 디렉터리 밖에서는 `--repo OWNER/REPO`를 지정한다. 원격 브랜치만 필요하면 `--no-checkout`을 추가한다.
3. 구현·검증 후 커밋·푸시는 Git 명령으로 처리한다.
4. `tools github pr create --prefix feat --title "add login" --body-file pr.md --json`을 실행한다. 현재 브랜치에서 이슈 번호를 읽어 PR에 연결한다. 본문에는 변경 내용과 실제 검증 결과를 포함한다.

`create`가 필요한 GitHub 조회를 처리하므로 사전에 `context`를 실행할 필요가 없다. 오류나 필요한 정보의 누락이 없으면 반환된 계획·결과를 사용하고 라벨·유형·템플릿을 다시 조회하지 않는다. 목록 자체를 보고 싶을 때만 `context`를 사용한다. 검토가 필요할 때 `--dry-run --json`을 추가하며, 생성 전에 항상 별도로 실행할 필요는 없다.

제목은 영어·숫자·ASCII 기호를 허용하며 대소문자를 보존한다. 소문자 변환은 브랜치 이름에 적용한다. 저장소의 특수한 prefix 매핑은 `.tools.json`에 둔다. 구조화된 본문이 필요하면 `--file JSON`을 사용한다. Git 없이 PR을 생성할 때는 `--repo`와 JSON의 `head`를 지정한다.

`partial` 결과의 URL을 보고 기존 항목을 수정한다. 브랜치 작업만 실패했다면 `tools github issue branch --number 123`으로 이어간다. 이슈를 다시 생성하지 않는다. 생성 요청 결과를 받지 못한 경우에도 원격 상태를 먼저 확인한다. 검증 명령은 기록이며 도구가 실행하지 않는다.
