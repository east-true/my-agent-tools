# 저장된 CI 원문 선택 조회

CI 발췌만으로 필요한 문맥을 읽을 수 없을 때 전체 수집 JSON의 `evidence_file`과 `evidence_sha256`을 사용한다. 아래 읽기 전용 옵션은 `ci failures`, `ci rerun`, `pr inspect`, `pr submit`, `pr merge`에서 같은 계약으로 동작한다. 인증·원격 수집·재실행 요청·푸시·머지·정리를 실행하지 않는다.

```sh
tools github ci failures --read-evidence EVIDENCE --evidence-sha256 SHA \
  --evidence-run 42 --evidence-attempt 2 --job 11 \
  --pattern 'specific context' --context 2 --json
```

전체 자료의 실제 SHA를 먼저 검증한 뒤 명시한 run·attempt·job의 보관 원문만 읽는다. `--range START:END`는 원래 job 로그의 1-based 포함 구간이다. `--pattern`은 리터럴 문자열이다. 한 응답에 담기 어려우면 선택 범위를 줄이거나 `--evidence-output-bytes`를 늘린다. 명시한 회차가 모호하거나 보관 원문이 없으면 오류로 반환한다. 컴파일 진단만 수집됐고 로그 원문이 보관되지 않은 결과에서 새 로그를 만들어 내지 않는다.

`segments`에는 자료 종류·스텝/위치·원래 시작/끝 줄·원문 배열과 `collection_truncated`가 포함된다. 중복 로그의 변형은 해당 occurrence와 연결된 `log_variants`를 선택한다. 정확한 시작 좌표로 구간을 표시하며 본문을 요약하거나 행 길이를 자르지 않는다.

반환한 `saved_observation=true`, `fresh_state_verified=false`는 이전 관찰을 읽었다는 뜻이다. `collection_complete`와 `collection_truncated`는 최초 수집의 완전성을 유지한다. 원문 파일의 SHA가 맞는다고 최신 CI 상태나 완전한 다운로드가 증명되는 것은 아니다. 원래 실행이 부분 수집이면 복구 결과도 이를 숨기지 않는다.

읽기 전용 분기에는 위 옵션과 `--json`만 사용할 수 있다. `--run`, `--number`, `--repo`, `--wait`, `--resume`, 쓰기 옵션 등을 함께 넣으면 동작 전에 거부한다. 일반 CI 조회에서 이미 충분한 진단이 나오면 이 절차를 추가할 필요가 없다. [완료 작업 비교](../../benchmarks/completion/README.md)는 직접 Python/gh 필터 처리도 허용하고 추가 조회·입력 작성 비용을 포함한다.
