# `tools fs apply`

```sh
tools fs apply --root . --plan edits.json --json
# 검증한 계획을 실제 반영
tools fs apply --root . --plan edits.json --apply --json
```

기본은 미리보기입니다. 이미 반영이 승인된 작업은 `--apply` 한 번으로 전체 검증과 반영을 수행하므로 별도의 미리보기 호출은 필요하지 않습니다. JSON 계획은 `version: 1`, 파일 1–200개입니다. 전체 파일의 경로·기존 SHA·UTF-8·크기·일치 횟수를 먼저 검증하며 한 항목이라도 잘못되면 아무 파일도 수정하지 않습니다.

```json
{
  "version": 1,
  "files": [
    {
      "path": "config/settings.txt",
      "sha256": "REPLACE_WITH_INSPECT_HASH",
      "replacements": [{"old": "limit=20", "new": "limit=40", "count": 1}]
    },
    {"path": "config/new.txt", "sha256": "absent", "content": "created\n"}
  ]
}
```

기존 파일은 `replacements` 또는 전체 `content` 중 하나를 사용합니다. `count`는 정확히 일치해야 하는 양의 횟수이며 여러 치환은 순서대로 적용합니다. SHA는 `inspect --hash`에서 받은 소문자 64자리 값을 사용합니다. `absent`는 새 파일용이며 부모 디렉터리는 미리 있어야 합니다. 기존 파일을 덮어쓰는 생성 요청은 거부합니다.

기존 파일의 권한 비트와 리터럴 치환 밖의 바이트·줄바꿈을 보존합니다. 새 파일은 같은 디렉터리의 완성된 임시 파일을 하드 링크로 게시하므로 생성 경쟁 시 덮어쓰지 않습니다. 하드 링크를 지원하지 않는 파일시스템은 생성이 실패합니다.

반영 직전 SHA를 다시 확인하고 파일별로 교체합니다. 여러 파일 전체의 원자적 트랜잭션이나 비협조적 외부 편집의 완전한 차단을 보장하지 않습니다. 중간 실패는 `partial`, 파일별 `applied`·`failed`·`not_applied`와 완료 건수를 반환합니다. 이미 반영한 파일을 자동 롤백하지 않으며 실제 결과를 확인해 다음 계획을 만듭니다. 협조적 잠금 `.tools-fs-apply.lock`과 임시 파일은 정상 종료 시 정리합니다.

반영 직후 각 파일을 다시 읽어 준비한 바이트·기존 권한과 비교합니다. 성공한 파일은 실제 바이트 SHA와 `verified: true`를 반환합니다. 재검증 실패는 `verification_failed`·`partial`로 표시하며 이미 반영한 파일은 완료 건수에 포함합니다.

권한 검증은 Go가 해당 OS에서 표현하는 파일 권한 비트 기준입니다. 별도 ACL·소유자·확장 속성의 보존까지 검증하는 기능은 아닙니다.

`--report-changes`를 지정하면 바뀐 현재 줄을 `ranges`로 함께 반환합니다. 반환된 값으로 확인할 수 있는 작업은 별도 inspect 호출이 필요 없습니다. 미리보기에서는 예정된 줄이며 `verified`는 실제 반영·재검증 후에만 참입니다. 큰 보고서는 줄 구간을 생략하고 `report_complete: false`로 표시합니다. 반영 상태·파일별 SHA·오류는 보존하므로 결과 메타데이터만으로 상한을 넘길 수 있습니다.

해시를 직접 복사하지 않으려면 같은 계획 형식에서 기존 파일의 `sha256`만 생략한 수정 조건을 `--spec`으로 전달합니다. 새 파일은 여전히 `sha256: absent`를 명시해야 하며, 없는 수정 대상을 자동 생성하지 않습니다.

동일한 치환을 여러 기존 파일에 적용할 때는 `--spec`의 `groups`에 경로 목록과 공통 치환을 한 번만 입력합니다. 횟수와 원본 SHA는 파일별로 검증합니다. 일부 파일만 횟수나 조건이 다르면 그 파일은 기존 `files` 형식으로 별도 지정합니다.

```json
{"version":1,"groups":[
  {"paths":["config/a.txt","config/b.txt"],"replacements":[{"old":"limit=20","new":"limit=40","count":1}]}
]}
```

`files`와 `groups`를 함께 사용할 수 있으며 `files` 뒤에 그룹·경로의 입력 순서대로 펼칩니다. 펼친 뒤에도 최대 200개이고 중복 경로를 거부합니다. 그룹은 기존 파일의 치환 전용이며 생성은 `files`에서 명시합니다. 저장 계획은 그룹 없이 기존 `version`·`files`·개별 SHA 형식으로 유지하므로 `--plan`에서 그대로 사용할 수 있습니다. 그룹을 `--plan`으로 직접 반영하지는 않습니다.

```json
{"version":1,"files":[
  {"path":"config/settings.txt","replacements":[{"old":"limit=20","new":"limit=40","count":1}]}
]}
```

```sh
tools fs apply --spec replacements.json --save-plan validated-plan.json --report-changes --json
# 계획 생성·전체 검증·저장·반영·재검증을 한 번에 수행
tools fs apply --spec replacements.json --save-plan another-plan.json --apply --report-changes --json
```

`--save-plan`은 선택 사항이며 생성한 원본 SHA 계획을 새 파일로 저장합니다. 기존 계획을 덮어쓰거나 수정 대상으로 계획을 저장하는 요청은 거부합니다. 저장 실패 시 반영하지 않습니다. 저장된 계획은 이후 `--plan`으로 독립 사용하고 원본이 바뀌면 거부합니다. 이미 적용한 계획을 재실행하려면 새 조건과 계획을 만들어야 합니다. `--plan`과 `--spec`은 함께 사용하지 않습니다.

저장 계획은 실제 파일 바이트를 반영 전후로 다시 확인합니다. `saved_plan`에 `version`, `files`(계획의 파일 수), `sha256`(저장한 JSON 바이트), `verified`를 반환하므로 생성·저장 결과를 확인하기 위한 별도 계획 파일 조회가 필요 없습니다. 전체 원본 SHA와 수정 조건은 저장 파일에 유지합니다. 최초 저장 재검증 실패는 반영을 중단하고, 반영 후 재검증 실패는 `saved_plan.verified: false`·오류·`partial`로 반환하며 이미 반영한 파일을 롤백하지 않습니다.

[연속 작업과 검증 근거를 활용하는 방법](agent-usage.md)

특정 치환 횟수를 실제 관찰값으로 고치라는 지시가 있으면 `--recount FILE_INDEX:REPLACEMENT_INDEX`를 사용합니다. 두 번호는 펼친 파일 배열과 해당 파일의 `replacements` 배열에서 1부터 시작합니다. 그룹을 쓰면 기존 `files` 뒤에 그룹의 경로 순서대로 번호를 셉니다. 옵션은 반복할 수 있고 `--spec`에서만 사용할 수 있습니다.

```sh
# 첫 번째 파일의 첫 번째 치환 횟수만 재계수하도록 승인된 작업
tools fs apply --spec replacements.json --recount 1:1 --save-plan validated-plan.json --apply --report-changes --json
```

원본 JSON을 바꾸지 않고 복사본의 지정한 횟수만 수정합니다. 원래 기대·실제 횟수와 경로·치환 번호는 `corrections`에 반환하며 진단→수정 계획→저장→반영→재검증이 이어집니다. 앞선 치환이 적용된 순서상의 내용으로 재계수합니다. 관찰한 원본 SHA를 계획에 고정하므로 계수 후 파일이 바뀌면 반영하지 않습니다.

0건 일치·빈 `old`·0 이하의 입력 `count`·다른 항목의 횟수 불일치·없는 인덱스·생성 대상은 이 옵션으로 허용하지 않습니다. SHA·경로·UTF-8·내용·크기·기존 파일 생성 금지 검증도 그대로 수행합니다. 기본 동작은 정확한 횟수 검증이며 지정하지 않은 횟수를 자동으로 바꾸지 않습니다.

수정 조건 오류에는 파일 경로를 함께 반환합니다. 정확한 치환 횟수가 다르면 구조화된 `diagnostic`에 `path`, `replacement_index`(1부터 시작), `expected_count`, `actual_count`, `code`, `message`를 담습니다. `--spec` 계획 생성 실패와 `--plan` 파일별 검증 실패 모두 같은 진단 형식을 사용합니다.

```json
{"path":"config/settings.txt","code":"replacement_count","replacement_index":1,"expected_count":2,"actual_count":1,"message":"replacement count differs from plan"}
```

진단만으로 잘못된 항목을 찾고 조건을 바로잡을 수 있습니다. 오류가 난 전체 배치는 반영하지 않으며, 횟수를 자동으로 완화하지 않습니다. 다른 SHA·경로·치환 조건도 다시 전체 검증합니다.

`--spec`은 독립 파일의 오류를 모아 반환합니다. 두 개 이상이면 `diagnostics` 배열에 각 오류를 넣고 기존 `diagnostic`은 첫 오류로 유지합니다. 잘못된 치환 뒤의 같은 파일 내용은 추측하지 않고 그 파일의 첫 오류에서 멈춥니다. 다른 파일 검증은 이어가며, 오류가 있으면 사용 가능한 계획을 반환하거나 저장·반영하지 않습니다. 취소·파일/배치 상한에서는 모든 오류 수집을 보장하지 않습니다. 원본 읽기와 준비된 결과는 각각 배치 32 MiB 상한을 적용합니다.

[공통 옵션](README.md) · [실측](../benchmarks/fs/apply.md)

## 큰 결과의 원문 복구

일반 크기는 한 응답으로 반환하며 생략된 단일 관찰은 검증된 `saved_report`로 복구한다. 저장·읽기 전용 선택·현재 파일 재확인과 기준/적용 상태의 의미는 [공통 보고서 계약](reports.md)을 따른다. `--save-report`로 크기와 관계없이 보관할 수도 있다.

`--report-pattern TEXT --report-context N`은 실제 요청한 변경 구간을 최초 응답에서 선택한다. 검증된 파일의 `before_mode`, `mode`, `mode_preserved`도 반환하므로 기존 권한을 확인하기 위해 도구를 다시 실행할 필요가 없다. 새 파일에는 기존 권한 보존을 주장하지 않는다. 상세 계약은 [보고서 선택·복구](reports.md)를 따른다.
