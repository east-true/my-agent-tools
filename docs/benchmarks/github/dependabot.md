# github dependabot list / view

작업 도중 README에 추가된 `tools github dependabot list / view` 행을 별도로 실행했습니다. 열린 high/critical 경고의 모든 페이지를 조회한 뒤 경고 7의 설명·참고 링크를 조회하는 **하나의 에이전트 작업**을 비교합니다. 각 하위 명령의 단독 비용은 아닙니다.

2026-10-07 Linux, `gpt-6.1-sol`, high 추론, 방식별 새 세션 3회이며 실행 순서는 `gh → tools → tools → gh → gh → tools`입니다. 프로덕션 CLI의 API 의존성만 주입하고 실제 `gh api`도 같은 로컬 실험 API로 연결했습니다. 두 경고를 한 항목씩 두 커서 페이지로 강제 분할한 작은 합성 실험이며 실제 보안 경고를 조회하지 않습니다.

| 평균 또는 범위 | 직접 `gh` | `tools` |
|---|---:|---:|
| 총 토큰 | 31,209 | 30,715 |
| 캐시 제외 입력 | 16,009 | 15,669 |
| 캐시 입력 | 14,848 | 14,720 |
| 출력 토큰 | 351 | 326 |
| 쉘 명령 항목 | 2 | 2 |
| 실행 시간(초) | 16.04 | 14.90 |
| 총 토큰 범위 | 31,159–31,282 | 30,619–30,880 |

총 토큰 차이는 −1.6%, 캐시 제외 입력은 2.1% 적었습니다. 큰 차이는 관찰되지 않았습니다. 모델 없는 사전 검증에서 실제 CLI와 `gh --paginate`가 두 페이지를 모두 읽었으며, 모델 6회도 최종 JSON·페이지 조회·상세 조회·API/Git/작성 파일 상태 보존 검증을 통과했습니다.

각 경고의 패치 버전은 `security_vulnerability.first_patched_version`을 사용해야 합니다. 합성 advisory에 다른 릴리스 계열의 `9.0.0`을 넣고 올바른 `2.0.1`과 구분했으며, 패치 정보가 없는 두 번째 경고는 `null`을 유지했습니다. 모든 데이터는 테스트용이며 취약점이나 업데이트 권고가 아닙니다.

직접 방식도 `--jq`·명령 묶음·로컬 필터를 사용할 수 있었고 설치된 `gh`에서 지원하는 옵션과 `python3` 명령 이름을 안내했습니다. 캐시는 통제하지 않았고 실제 GitHub 권한·오류·서비스 지연·대규모 경고 목록은 측정하지 않았습니다. 금액 절감을 입증하지 않습니다. `gh --jq`로도 동일한 필드 투영을 제공할 수 있습니다.

## 재현

Go·Git·`gh`·Codex와 모델 인증이 준비된 Linux/WSL에서 새 출력 경로를 사용합니다. 첫 실행은 모델 없는 사전 검증, 두 번째 실행은 최대 6회 모델 측정입니다.

```sh
python3 scripts/benchmarks/run_dependabot_benchmark.py --root /tmp/dependabot-preflight
python3 scripts/benchmarks/run_dependabot_benchmark.py --root /tmp/dependabot-measurement --run-models
```

프롬프트·합성 입력·소스 SHA·실행별 토큰·명령·API 요청·검증 결과는 [원본 JSON](dependabot.json)에 있습니다. 다른 6개 작업은 [공통 실험](command-suite.md)에 별도로 기록했습니다.
