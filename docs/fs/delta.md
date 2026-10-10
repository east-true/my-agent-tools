# `tools fs delta`

```sh
tools fs delta --root . --include '**/*.go' --state-file .tools/state/fs/go.json --include-content --json
# 같은 선택으로 반복하면 변경분만 반환합니다.
tools fs delta --root . --include '**/*.go' --state-file .tools/state/fs/go.json --include-content --context 2 --json
```

첫 호출은 `initialized`와 추적 파일 수를 반환하고 기준을 저장합니다. 다음 호출부터 `changes`에 `added`, `modified`, `deleted`를 반환합니다. 바뀐 내용이 없으면 `unchanged`입니다. 크기·수정 시각이 같아도 내용을 다시 해시하므로 변경을 확인합니다. rename은 삭제+추가로 표시합니다.

`--include-content`를 사용하면 UTF-8 원문을 기준 파일에도 저장하고 변경 파일의 이전·현재 줄 구간을 반환합니다. 떨어진 수정은 여러 구간으로 나누고 `--context` 문맥이 겹치면 합칩니다. 고유한 공통 줄로 구간을 나누고 반복 줄은 최대 1,048,576셀의 LCS로 비교합니다. 그 상한을 넘는 반복 구간은 더 넓은 구간이 될 수 있습니다. `before`와 `ranges`는 각각 원본·현재 줄 번호이며 비어 있는 쪽은 생략하므로 배열 위치로 무조건 짝짓지 않습니다. 출력의 SHA는 원래 바이트 기준입니다. 바이너리는 해시만 비교합니다.

기준은 루트·선택·파일 상한·내용 저장 여부와 연결됩니다. 다른 조건은 별도 `--state-file`을 사용하거나 유효한 같은 루트 기준에 `--reset`을 명시합니다. 상태 파일과 잠금 파일은 조회에서 자동 제외합니다. 손상되거나 다른 루트의 기준은 덮어쓰지 않습니다.

누락·읽기 오류·결과 상한 초과 시 기준을 갱신하지 않습니다. 총 수집 바이트는 32 MiB, 기준 JSON은 64 MiB로 제한합니다. `<state-file>.lock`은 협조적 동시 실행 방지용이며 중단된 잠금은 상태를 확인한 후 직접 처리합니다. 기준 파일은 내용·경로를 포함할 수 있으므로 로컬 상태 경로에 보관합니다.

[공통 옵션](README.md) · [실측](../benchmarks/fs/delta.md)

같은 변경분을 여러 번 확인하거나 후속 처리 실패 시 다시 읽으려면 `--peek`를 사용합니다.

```sh
tools fs delta --include '**/*.go' --state-file .tools/state/fs/go.json --include-content --peek --json
```

이미 초기화한 기준이 필요하며 `--reset`과 함께 사용할 수 없습니다. `--peek`는 기준 파일 바이트를 갱신하지 않고 `state_updated: false`를 반환합니다. 잠금은 기존과 같이 사용합니다. 처리를 마친 뒤 일반 delta 호출로 기준을 갱신하면 됩니다.

수정 본문만 필요하면 `--include-content --content-kinds modified`를 사용합니다. `added`·`modified`·`deleted`를 반복 지정할 수 있으며 기본은 모든 종류의 본문입니다. 선택하지 않은 종류도 경로·종류·SHA와 추적 건수는 반환하고 원문 기준은 그대로 저장합니다. 이 옵션은 보고서 본문 선택이므로 같은 기준에 적용하거나 `--peek`와 함께 사용할 수 있습니다.

반복 관찰까지 한 호출로 수행하려면 `--peek --comparisons N`을 사용합니다. `N`은 1–10이며 기본은 1입니다. 2 이상은 초기화된 기준과 `--peek`가 필요하고 `--reset`을 허용하지 않습니다.

```sh
tools fs delta --include '**/*.go' --state-file .tools/state/fs/go.json \
  --include-content --content-kinds modified --peek --comparisons 2 --json
```

매번 실제 파일을 새로 읽고 전체 선택 파일의 해시·권한·원문 상태를 비교합니다. 이전 결과를 재사용하지 않습니다. 결과가 같으면 `comparisons`, `consistent: true`, `baseline_preserved: true`와 완전한 변경 보고서를 한 번만 반환합니다. 기준 보존은 관찰 전후의 실제 기준 파일 바이트로 확인합니다. 반복 사이의 대기 시간은 없으므로 중간에 다른 작업을 수행해야 하면 독립 `--peek` 호출을 사용합니다.

관찰한 상태가 다르면 `status: unstable`, `complete: false`, `consistent: false`와 각 고유 상태의 `snapshot_sha256`·`other_results`를 반환합니다. 첫 보고서도 유지하며 서로 다른 결과를 숨기지 않습니다. 수정된 기존 파일의 권한만 바뀐 경우에는 `before_mode`·`mode`로 차이를 표시합니다. 읽기 오류·보고서 상한은 여전히 불완전한 결과이고 기준을 갱신하지 않습니다. 반복 스캔의 I/O 비용은 절감하지 않습니다.

## 큰 결과의 원문 복구

일반 크기는 한 응답으로 반환하며 생략된 단일 관찰은 검증된 `saved_report`로 복구한다. 저장·읽기 전용 선택·현재 파일 재확인과 기준/적용 상태의 의미는 [공통 보고서 계약](reports.md)을 따른다. `--save-report`로 크기와 관계없이 보관할 수도 있다.

`--report-pattern TEXT --report-context N`은 변경 경로·종류·SHA를 유지하면서 이전/현재 원문의 필요한 위치·CRLF를 최초 응답에 반환한다. `--include-content`가 필요하며 `--content-kinds`를 따른다. 선택을 완전히 반환한 일반 조회는 기준을 갱신할 수 있으므로 보존하려면 `--peek`를 쓴다. [보고서 선택·복구](reports.md)를 참고한다.

단일 `--peek`도 원래 기준 파일의 실제 바이트를 다시 읽고 `baseline_sha256`, `baseline_preserved`를 반환한다. 확인 중 기준이 바뀌거나 사라지면 부분 결과로 보고한다. 명시한 경로는 부재 상태에서도 추적할 수 있어 최초 생성과 삭제 후 반복 조회를 처리한다.
