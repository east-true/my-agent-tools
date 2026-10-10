# 큰 보고서의 원문 복구

`fs test-results`, `fs delta`, `fs apply`의 일반 크기 결과는 한 응답으로 반환한다. 기본 64 KiB를 넘는 진단·변경 구간은 생략 수와 필요한 합계·경로·SHA·적용 영수증을 남긴다. 단일 관찰의 생략 원문은 `--root` 아래 `.tools/state/fs-reports`에 자동 저장하고 `saved_report.path`, `sha256`, `verified`로 반환한다. 저장 실패는 경로·오류와 기존 적용 결과를 보존하며 부분 실패로 보고한다.

`--save-report FILE`은 크기와 관계없이 그 작업의 전체 JSON 관찰을 별도 파일에 보관한다. 기존 파일을 덮어쓰지 않는다. apply에서 지정하는 저장 파일의 부모 디렉터리는 있어야 하며, 수정 대상과 같은 경로를 사용할 수 없다. 자동 저장은 os.Root 경계로 루트 밖의 심볼릭 링크를 따라 쓰지 않는다. 자동 저장 디렉터리는 기본 조회 범위에서 제외된다. 보고서는 자동 만료하지 않으므로 필요가 끝난 자료는 사용자가 삭제할 수 있다. 모델 입력 캐시나 같은 명령의 연속 사용은 필요하지 않다.

필요한 구간을 조회 전에 알면 최초 응답에서 요청한 원문과 검증 근거를 함께 받을 수 있다. 출력 선택은 사용자가 명시하며 합계·변경 경로·SHA·적용 상태는 유지한다. 일치 구간이 없는 것도 결과로 표시되며 생략 원문을 자동으로 요약하거나 수정 의무를 추론하지 않는다.

```sh
tools fs test-results --path TEST-unit.xml --diagnostic-pattern 'AssertionError' --diagnostic-context 2 --json
tools fs delta --state-file STATE --include-content --report-pattern 'old value' --report-context 2 --peek --json
tools fs apply --plan PLAN --apply --report-changes --report-pattern 'target line' --report-context 2 --json
```

JUnit은 `diagnostics_selected`와 원래 `diagnostic_index`·`details_ranges.start/text`를 반환한다. 합계·suite·XML SHA와 수집 오류를 보존한다. delta는 `report_selected`와 변경별 `before_raw_ranges`·`raw_ranges`의 원래 위치·정확한 CRLF를 반환한다. `--content-kinds` 선택도 유지한다. 단일 `--peek`는 실제 기준 파일 read-back의 `baseline_sha256`·`baseline_preserved`도 제공한다. apply는 `report_selected`와 요청한 변경 줄, 실제 read-back의 `before_mode`, `mode`, `mode_preserved`를 함께 제공한다. 기존 파일의 권한 보존은 `verified`와 함께 확인되며 새 파일에 과거 권한이 있었다고 주장하지 않는다.

문맥 옵션은 0–1000이고 해당 패턴이 필요하다. 기본 전체 보고서 기능은 유지한다. 선택 결과가 상한 안에 들어가면 자동 보고서 파일을 만들거나 후속 읽기를 요구하지 않는다. 전체 보관도 원하면 `--save-report`로 선택 전의 자료를 저장한다. delta에서 명시한 선택을 완전히 전달하면 일반 기준 갱신 규칙을 따르며 기준 유지가 필요한 조회는 `--peek`를 쓴다.

생략 원문은 같은 명령의 읽기 전용 분기로 필요한 부분만 복구한다.

```sh
tools fs test-results --read-report REPORT --report-sha256 SHA --pattern 'AssertionError' --context 2 --json
tools fs delta --read-report REPORT --report-sha256 SHA --path deleted.txt --pattern 'old value' --json
tools fs apply --read-report REPORT --report-sha256 SHA --path settings.py --range 80:85 --json
```

`--read-report`는 원격 조회·원래 작업의 재실행·수정·기준 갱신을 하지 않는다. 입력 파일의 실제 SHA를 먼저 확인하고, 파일·패턴·구간을 선택한 결과만 반환한다. `--pattern`은 하나의 리터럴 문자열이며 `--range`는 1부터 시작하는 포함 구간이다. 원문 구간을 읽기 위한 결과 상한은 `--max-output-bytes`로 조정할 수 있다. 선택한 결과도 너무 크면 더 좁은 선택이나 큰 상한이 필요하다는 오류를 반환한다. 다른 실행·선택·쓰기 옵션과 섞으면 실행 전에 거부한다.

- test-results: 원래 합계·suite·XML SHA를 유지한다. 선택한 진단은 `diagnostic_index`, `details_ranges`의 원래 줄 번호·정확한 디코딩 원문과 `details_selected`로 표현한다. 저장 관찰에 포함된 보고서의 SHA를 다시 확인하며 XML이 바뀌면 거부한다. `execution_verified`는 계속 false이다.
- delta: 생략된 변경 경로·종류·before/after SHA는 최초 응답에 남는다. 복구할 때 당시 선택된 파일 목록·SHA·권한과 삭제 경로의 부재를 재확인한다. `before_raw_files`는 저장된 이전 원문의 CRLF·마지막 개행과 위치를 보존한다. 전송이 불완전했던 기준은 갱신하지 않으며 복구도 항상 `state_updated=false`이다. 다중 `--comparisons`의 합성 보고서 복구는 현재 지원하지 않으며 `--save-report`와 함께 사용하면 실행 전에 거부한다.
- apply: 적용 완료·파일별 `verified`·SHA·부분 실패는 최초 응답에 남는다. 읽기는 저장된 적용 당시의 보고서이며 `current_files_verified=false`이다. 이후 편집을 덮어쓰지 않는다. 저장된 변경 `ranges`는 기존 변경 보고서의 줄 표현이며 현재 파일의 raw 바이트 조회를 대신하지 않는다.

`saved_observation`과 `observed_at`은 자료가 생성된 시점을 나타낸다. test-results·delta의 `current_files_verified`는 읽는 동안 해당 관찰의 파일 근거를 재확인했다는 뜻이며 이후 다른 프로세스의 변경을 막는 보장은 아니다. `collection_complete`는 저장된 수집/적용 상태이다. 선택 읽기의 성공을 원래 테스트 실행이나 부분 적용의 성공으로 해석하지 않는다.

이 기능의 목적은 완료한 작업을 다시 실행하거나 큰 JSON을 해석하는 코드를 새로 작성하는 부담을 줄이는 것이다. 저장·읽기·검증 비용이 있으므로 직접 처리보다 항상 효율적이라고 보장하지 않는다. 실제 완료 작업의 비교는 [변경 작업 벤치마크](../benchmarks/completion/README.md)에 별도로 기록한다.
