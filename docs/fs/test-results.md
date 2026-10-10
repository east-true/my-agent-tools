# `tools fs test-results`

JUnit XML의 전체 수·실패·오류·skip과 testcase 진단을 읽습니다. 보고서를 순회하고 합산하는 코드를 매번 작성하지 않아도 됩니다. Git·GitHub 인증은 필요하지 않습니다.

```sh
tools fs test-results --root . --json
tools fs test-results --include "order/build/test-results/**/*.xml" --include "delivery/build/test-results/**/*.xml" --json
tools fs test-results --path build/test-results/test/TEST-example.xml --json
```

기본 선택은 `**/TEST-*.xml`입니다. 명시한 `--include`·`--path`·`--exclude`와 [공통 파일·바이트 상한](README.md)을 따르며 읽기만 수행합니다. 테스트를 실행하거나 기존 보고서를 삭제하지 않습니다.

## 결과와 완전성

최상위 `tests`, `failures`, `errors`, `skipped`는 유효한 보고서의 합계이며 `report_count`는 그 보고서 수입니다. `reports`에는 경로, 실제 바이트 SHA-256, 관찰한 수정 시각, suite별 수와 진단이 있습니다. 중첩 suite의 부모 합계를 다시 더하지 않으며 부모에 직접 속한 testcase는 별도로 포함합니다. 같은 보고서를 여러 경로에 복사해 선택하면 별개의 입력으로 계산하므로 중복 복사본은 선택에서 제외합니다.

`diagnostics`는 실패·오류·skip마다 suite, testcase 이름, 선택적 classname, `kind`, message, type, details를 유지합니다. 내용은 XML에서 디코딩한 텍스트이며 XML markup의 바이트 복제본은 아닙니다. 보고서 경로와 SHA로 원본을 구분합니다. 한 testcase에 여러 실패 진단이 있으면 모두 보존하고 실패 수는 그 testcase를 한 번 셉니다.

`testsuite`·`testsuites` 루트와 중첩 `testsuite`, testcase의 `failure`·`error`·`skipped`를 지원합니다. 선언된 수가 testcase/하위 suite 근거와 다르거나 XML·지원 구조가 잘못되면 해당 보고서를 합계에서 제외하고 `problems`에 경로와 오류를 반환합니다. 지원하지 않는 진단 안의 중첩 markup을 조용히 버리지 않습니다. 선언된 실패·오류·skip에 testcase 진단이 없는 집계 전용 보고서는 수를 유지하되 진단 부재를 불완전한 결과로 표시합니다.

보고서 없음·선택 누락·읽기 오류·관찰 중 파일 변경도 `partial`, `complete: false`, 종료 코드 1입니다. 유효한 다른 보고서는 함께 반환합니다. 오류가 난 보고서를 0개 성공으로 해석하지 않습니다.

큰 진단이 출력 상한을 넘으면 합계·보고서 근거는 유지하고 suite/진단을 생략합니다. `complete: false`와 생략 이유를 반환하며 해당 진단에 `diagnostics_unavailable: true`를 표시합니다. 전체 자료가 필요하면 `--max-output-bytes`를 높입니다. 현재 이 명령은 커서를 제공하지 않으며 필수 메타데이터만으로도 작은 상한을 넘길 수 있습니다.

## 이번 실행의 증명과 구분

`status: "observed"`, `complete: true`와 종료 코드 0은 선택한 보고서를 완전히 읽었다는 뜻입니다. 테스트 성공 여부는 `failures`, `errors`, `skipped`와 실제 실행 결과를 함께 확인합니다. 실패가 담긴 유효한 보고서도 읽기 자체는 성공할 수 있습니다.

`execution_verified`는 항상 `false`입니다. 먼저 테스트 프로세스 종료·종료 코드와 필요한 실행 근거를 확인한 뒤 이 명령으로 집계합니다. 기존 보고서가 있다는 이유만으로 이번 실행에서 테스트가 수행되거나 완료됐다고 판단하지 않습니다. 여러 파일의 조회가 단일 실행의 원자적 스냅샷이라는 보장도 없습니다.

```sh
# 알고 있는 실행 시작 시각보다 오래된 보고서도 표시
tools fs test-results --include "build/test-results/**/*.xml" --after 2026-10-10T15:00:00Z --json
```

`--after`는 RFC3339 시각과 파일 mtime을 비교합니다. 오래된 보고서는 합계·SHA와 함께 보존하고 `partial`로 표시합니다. 수정 시각이 최신이어도 실행 완료나 특정 실행 소속을 증명하지 않습니다. 캐시로 재사용된 테스트 보고서도 임의로 삭제하지 않습니다.

[실제 사용 기록과 개선 근거](../usage-based-improvements.md) · [공통 안내](README.md)

## 큰 결과의 원문 복구

일반 크기는 한 응답으로 반환하며 생략된 단일 관찰은 검증된 `saved_report`로 복구한다. 저장·읽기 전용 선택·현재 파일 재확인과 기준/적용 상태의 의미는 [공통 보고서 계약](reports.md)을 따른다. `--save-report`로 크기와 관계없이 보관할 수도 있다.

`--diagnostic-pattern TEXT --diagnostic-context N`으로 필요한 진단 원문·원래 위치를 최초 응답에서 선택할 수 있다. 모든 합계·suite·보고서 SHA·XML 오류를 유지하고 `diagnostics_selected`로 선택 범위를 표시한다. [보고서 선택·복구](reports.md)를 참고한다.
