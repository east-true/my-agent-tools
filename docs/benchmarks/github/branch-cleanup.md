# github branch cleanup

측정 대상은 `tools github branch cleanup --apply`의 대상 분류와 실제 참조 삭제입니다. 미리보기만 수행하는 명령은 개별 측정하지 않았습니다.

2026-10-07에 `gpt-6.1-sol`, high 추론, 방식별 새 세션 3회로 직접 `gh + git` 처리와 비교했습니다. 실제 `gh`, API 의존성만 교체한 프로덕션 CLI 로직, 로컬 GitHub 실험 API, 임시 Git 저장소를 사용했습니다.

| 평균 | 직접 `gh + git` | `tools` |
|---|---:|---:|
| 총 토큰 | 70,552 | 31,006 |
| 캐시 제외 입력 토큰 | 28,414 | 16,102 |
| 쉘 명령 항목 | 5.33 | 1 |

총 토큰은 56.1%, 캐시 제외 입력은 43.3% 적었습니다. 각 유효 실행은 로컬 브랜치 6개·원격 브랜치 5개·오래된 추적 참조 2개를 삭제하고 15개 대상을 보존했습니다. 비교한 6회 모두 최종 참조·SHA 삭제 조건·브랜치 설정·worktree·작업 파일이 기준과 일치했습니다.

최초 직접 처리 안내의 보호 범위가 모호했던 3회는 원본에 남기고 동등 비교에서 제외했습니다. 안내를 명확히 한 직접 처리를 3회 추가 측정해 기존 `tools` 결과와 비교했습니다. 캐시는 통제하지 않았고 수정된 기준은 나중에 실행했습니다. 금액 절감이나 다른 명령의 효과를 입증한 결과는 아닙니다.

## 재현

Linux/WSL에서 Go·Git·`gh`·Codex를 준비하고 새 출력 디렉터리를 사용합니다. 첫 명령은 모델 호출 없이 검증하며, 두 번째는 모델 실행을 최대 6회 수행합니다.

```sh
python3 scripts/benchmarks/run_branch_cleanup_benchmark.py --root /tmp/branch-cleanup-preflight
python3 scripts/benchmarks/run_branch_cleanup_benchmark.py --root /tmp/branch-cleanup-experiment --run-models
```

프롬프트·개별 실행·제외 사유·한계는 [원본 JSON](branch-cleanup.json)에 있습니다.
