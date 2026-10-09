# `tools github pr delta`

기준 커밋 이후 현재 PR head까지의 변경 파일을 조회합니다. 기본은 파일 메타데이터이며 patch는 요청했을 때만 출력합니다.

```sh
tools github pr delta --number 123 --since BASELINE_SHA --json
tools github pr delta --number 123 --since BASELINE_SHA --include-patch --json
```

`BASELINE_SHA`는 전체 40자리 SHA입니다. 조합 명령은 `--json` 없이도 JSON을 반환합니다. 파일명 순서로 정렬하며 rename의 `previous_filename`을 보존합니다.

## 검증과 결과

PR head를 고정하고 compare의 기준·merge base·전체 커밋 페이지·마지막 head를 검증합니다. 기준이 현재 head의 조상이 아니면 `needs_refresh`, `complete: false`로 반환합니다. force push 등으로 기준이 달라졌다면 전체 diff를 조회하거나 새 기준을 선택하세요.

변경 파일이 300개 이상이면 GitHub compare 상한에 걸렸을 가능성이 있어 `partial`, `complete: false`로 반환합니다. 이 경우 완전한 로컬 Git diff를 사용합니다. 조회 중 PR head가 바뀌어도 부분 결과로 표시합니다. [GitHub compare API](https://docs.github.com/en/rest/commits/commits#compare-two-commits).

JSON은 `status`, `repo`, `number`, `since`, `head`, `complete`, `files`, 선택적 `notes`를 포함합니다. 각 파일은 `filename`, `status`, `additions`, `deletions`, 선택적 `previous_filename`을 반환합니다. `--include-patch`를 주면 API가 제공하는 `patch`도 포함합니다. 바이너리 등 patch가 없는 파일은 해당 필드를 생략합니다.

| 옵션 | 의미 |
|---|---|
| `--number NUMBER` | 양의 PR 번호, 필수 |
| `--since SHA` | 기준 커밋의 전체 SHA, 필수 |
| `--repo OWNER/REPO` | 기본은 Git `origin` |
| `--include-patch` | patch 포함, 기본 `false` |
| `--compact`, `--artifact-dir DIR` | 선택적 출력 축소와 원문 보존 |

완전한 비교는 종료 코드 `0`, 부분 비교·기준 갱신 필요·API 오류는 `1`, 잘못된 입력은 `2`입니다. Fine-grained 토큰에는 Pull requests와 Contents 읽기 권한이 필요합니다.

## 효율 판단

기본 출력은 patch를 생략하고 검증·페이지 처리·출력 선택을 묶습니다. 직접 `gh --jq`도 필요한 메타데이터를 필터링할 수 있습니다. [최신 작업 전체 측정](../../benchmarks/github/pr/delta.md)은 기준 SHA 검증과 rename을 포함한 대표 시나리오의 결과이며 모든 단순 조회의 절감률을 뜻하지 않습니다.

[공통 안내](../README.md) · [PR 전체 상태 조회](inspect.md)
