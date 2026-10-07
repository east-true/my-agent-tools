# 반복 CI 코드 수정

실제 Go 코드 수정 두 건을 반복하는 로컬 PR/CI 실험입니다. 원격 GitHub PR·CI 실행·댓글·브랜치를 생성하는 모델 실험은 아니며, 실제 `tools github` 하위 명령의 측정도 아닙니다.

2026-10-07에 `gpt-6.1-sol`, high 추론, 방식별 워크플로 3회로 비교했습니다. 각 워크플로에 코드 수정 2건이 포함됩니다.

| 워크플로 평균 | 로그를 읽는 에이전트 | 보고서·코드를 받은 에이전트 | 코드만 반환하는 모델 |
|---|---:|---:|---:|
| 총 토큰 | 96,631 | 94,441 | 30,362 |
| 캐시 제외 입력 토큰 | 9,658 | 9,016 | 30,018 |

코드만 반환하고 호출자가 적용·테스트한 방식은 로그를 읽는 방식보다 총 토큰이 68.6% 적었습니다. 하지만 캐시 적중이 0이어서 캐시 제외 입력이 증가했으며 금액 절감은 입증하지 못했습니다. 수정 18건 모두 독립 테스트를 통과했습니다.

모든 방식에 동일한 내부 게이트를 적용했습니다. 워크플로별 상태 8개 중 중복·통과 상태 6개는 모델 호출 없이 처리했고, 실패 작업은 독립 검증을 통과한 뒤에만 확인 처리했습니다.

## 재현

새 출력 디렉터리를 사용합니다. 첫 명령은 모델 실행을 최대 12회, 두 번째는 최대 6회 수행합니다.

```sh
python scripts/benchmarks/run_workflow_benchmark.py --output /tmp/workflow-experiment --run-models
python scripts/benchmarks/run_worker_benchmark.py --output /tmp/worker-experiment --cache /tmp/workflow-experiment/cache
```

프롬프트·개별 실행·검증·별도 실제 GitHub CI 연동 확인·한계는 [원본 JSON](workflow.json)에 있습니다.
