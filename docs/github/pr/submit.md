# `tools github pr submit`

현재 브랜치의 기존 커밋을 푸시하고, 같은 브랜치의 열린 PR을 재사용하거나 새로 생성한 뒤 검사 결과를 조회합니다. 커밋 작성과 머지는 수행하지 않습니다.

```sh
tools github pr submit --prefix fix --title "handle duplicate requests" --body-file pr.md --json
tools github pr submit --prefix fix --title "handle duplicate requests" --body-file pr.md --wait=false --json
tools github pr submit --file pr.json --dry-run --json
```

## 처리 순서

1. 제목·본문·프로젝트 정책·현재 브랜치·fetch/push 원격을 검증합니다. 브랜치는 제목 prefix와 이슈 번호 규칙을 따라야 하며 작업 폴더는 깨끗해야 합니다. 푸시 전에 기존 PR·base 브랜치 존재·템플릿·기존 라벨·연결 이슈도 검증합니다. 명시한 base가 기존 PR의 base와 다르면 푸시하지 않습니다. base를 생략하면 기존 PR의 base 또는 저장소 기본 브랜치를 사용합니다.
2. 확인한 커밋 SHA만 대상 브랜치에 일반 푸시합니다. force push나 커밋 작성은 하지 않습니다. 원격 브랜치가 같은 SHA인지 다시 확인합니다.
3. 해당 브랜치의 열린 PR과 head·base를 다시 확인합니다. 하나가 있으면 재사용하며 제목·본문을 수정하지 않습니다. 없으면 사전 검증한 [pr create](create.md)의 계획으로 생성합니다. 여러 PR이 있거나 처음 선택한 PR이 사라졌다면 새 PR을 만들지 않습니다.
4. 기본으로 [pr inspect](inspect.md)를 호출해 검사 완료를 기다리고 리뷰·실패 자료를 반환합니다. `--wait=false`는 제출 후 대기를 생략합니다. 리뷰가 아직 없거나 변경 요청이 남아 있어도 검사 성공 여부와 구분합니다.

`--dry-run`은 로컬 입력·상태만 검증하고 `planned`로 반환합니다. 원격 PR 생성 계획이나 원격 최신 상태까지 보장하지 않습니다. 원격 푸시·PR 생성 요청은 자동 재시도하지 않습니다.

PR 준비에는 저장소·기존 라벨·PR 템플릿과 필요한 연결 이슈를 조회하고 이슈 생성에 필요한 이슈 유형 목록은 조회하지 않습니다. 제출 후 실패 자료는 다른 명령과 [완료 회차 캐시](../README.md#조회-재사용과-호출-제한)를 공유합니다.

## 옵션과 결과

입력은 `--prefix`, `--title`, `--body-file` 또는 `--file JSON`입니다. 파일에 `-`를 주면 stdin을 읽습니다. JSON 필드와 본문 정책은 [pr create](create.md), 설정은 [공통 안내](../README.md)를 따릅니다.

| 옵션 | 의미 |
|---|---|
| `--repo OWNER/REPO`, `--remote NAME` | 저장소와 일치하는 원격, 기본 `origin` |
| `--config FILE` | 프로젝트 설정 |
| `--dry-run` | 로컬 계획만 반환 |
| `--wait=false` | 제출 후 검사 대기 생략 |
| `--sections checks,reviews,failures` | 제출 후 수집 범위. 기본 `all`; 리뷰만 선택하려면 `--wait=false` |
| `--timeout`, `--interval` | 검사 조회 제한 `10m`, 조회 간격 `10s` |
| `--annotations=false`, `--max-log-bytes N` | 실패 자료 수집 옵션 |
| `--compact=false`, `--artifact-dir DIR` | 전체 출력, 원문 저장 경로 변경 |
| `--token-encoding`, `--artifact-retention`, `--artifact-limit` | [토큰 비교와 원문 보관](../README.md#간결한-출력) |

항상 최종 JSON 한 개를 반환합니다. 결과의 `push_attempted`, `pushed`, `number`, `url`, `creation`, `inspection`, `reused`, `error`, `resume`으로 진행 위치와 복구 방법을 확인합니다.

`submitted`·`planned`는 종료 코드 `0`입니다. 새 검사가 실패하면 `failed`, 제출 이후 조회·생성 일부가 실패하면 `partial`, 푸시 결과가 불명확하면 `unknown`과 종료 코드 `1`입니다. 잘못된 입력은 `2`입니다. `inspection`의 `blocked`는 리뷰·규칙 상태이며 제출 성공과 별개입니다. 일부 단계가 실패해도 이미 푸시되거나 PR이 생성됐을 수 있으므로 원격 상태를 확인하고 기존 항목에서 이어갑니다.

푸시는 Git 전송 인증, PR 생성은 기존 생성 권한, 검사 조회는 [pr inspect](inspect.md)의 읽기 권한이 필요합니다. [간결한 출력](../README.md#간결한-출력) · [머지와 정리](merge.md)

## 저장된 CI 원문 읽기

발췌에 없는 문맥은 `--read-evidence`와 예상 SHA·run·attempt·job·구간/패턴으로 선택해 읽는다. 원래 작업과 인증을 실행하지 않는 [공통 읽기 전용 계약](../ci/evidence.md)을 따른다.
