# GitHub 명령별 추가 측정

README에서 비어 있던 `context`, `setup`, `issue create`, `issue branch`, `pr create`, `branch cleanup` 미리보기를 직접 실행한 실험입니다. 실행별 입력·캐시 입력·출력 토큰, 프롬프트, 명령, 실험 API 요청과 검증 결과는 공유 원본 `commands.json`에 보관합니다.

이 문서는 기존 `gpt-6.1-sol` 실험의 기록입니다. 이후 README의 9개 작업을 측정한 [Astra·Luna 비교](models.md)에 모델별 토큰·성공률·실패 진단과 재현 방법을 기록했습니다.

## 실행 조건

2026-10-07 Linux에서 실제 설치된 `gh`와 Git, 프로덕션 `tools` CLI를 사용합니다. `gpt-6.1-sol`, high 추론, 명령·방식마다 새 세션 3회입니다. 최초 36회의 방식 순서는 각 명령마다 `gh → tools → tools → gh → gh → tools`이며, 매번 실험 저장소와 API 상태를 초기화합니다. 양쪽에 사용 안내를 주며 명령 묶음·`gh --jq`·로컬 Python/쉘 스크립트를 허용합니다. 직접 방식에 미리 구현한 자동화 도우미는 제공하지 않습니다.

최초 직접 처리에서 설치되지 않은 `python` 명령을 사용하다 `python3`로 재시도한 사례를 발견했습니다. 이 환경 비용을 주된 비교에서 제외하도록 Python 3의 명령 이름을 안내에 명시하고, `context`를 제외한 직접 처리 작업을 다시 측정했습니다. 최초 PR의 `tools` 3회는 생성 상태가 맞았지만 결과 정규화 안내가 모호해 응답 제목에서 `fix:`가 빠졌습니다. 제목 안내를 명확히 하고 PR은 양쪽을 다시 측정했습니다.

총 54회 중 최종 비교는 36회입니다. `context`는 Python 재시도가 없으며 최초 6회를 사용합니다. `setup`·`issue create`·`issue branch`·미리보기는 보정한 직접 처리 12회와 변경하지 않은 `tools` 12회를 비교합니다. PR은 새로 실행한 6회를 비교합니다. 최초 기준 18회는 실패·제외 사유와 함께 원본의 `excluded_trials`에 보존합니다. 보정 기준은 나중에 실행되므로 시점·캐시 차이가 남습니다.

최초 미리보기 1회는 모델 작업과 상태 검증 후 집계 코드가 실험 API 로그의 선택 필드 차이로 중단됐습니다. 남은 실행을 저장된 원래 프롬프트로 재개했고, 해당 실행의 토큰·응답·검증은 저장된 원본에서 복구했습니다. 복구된 시간은 파일 수정 시각에 따른 근사치이며 프로세스 종료 코드는 미확인으로 표시합니다. 이 실행은 제외된 최초 기준에만 포함하며 최종 비교에는 사용하지 않습니다.

작업 도중 별도의 Dependabot 기능이 추가되어 후속 빌드의 `cli.go` 도움말·라우터와 `api.go`의 커서 API 지원이 달라졌습니다. 변경 내용을 확인했으며 기존 측정 명령의 실행 경로와 구현은 바뀌지 않았습니다. 단계별 소스 SHA와 차이는 원본의 `protocols`, `concurrent_source_changes`에 기록했습니다. 나중에 README에 추가된 Dependabot 행은 [별도 실험](dependabot.md)으로 측정하며 아래 54회에 포함하지 않습니다.

`codex exec --json`의 `turn.completed.usage`에서 총 토큰을 `input_tokens + output_tokens`, 캐시 제외 입력을 `input_tokens - cached_input_tokens`로 계산합니다. 캐시 입력도 총 토큰에 포함하며 추론 토큰을 출력 토큰에 다시 더하지 않습니다. 평균과 범위, 쉘 명령 항목 수를 함께 기록합니다. CLI 자체는 모델을 호출하지 않습니다. 준비·구현·분석 과정의 토큰은 제외합니다.

## 실험 API와 실제 변경

실험 API는 로컬 HTTP 서버입니다. Go build overlay로 CLI의 API 의존성만 주입하며 프로덕션 명령 구현은 수정하지 않습니다. `gh api`도 실제 바이너리를 같은 서버로 연결합니다. 생성 요청은 서버의 이슈·PR 상태를 바꾸고, Development 연결 생성은 실제 임시 bare 저장소의 브랜치 참조도 생성합니다. Git fetch와 체크아웃은 실제 Git이 수행합니다. GitHub 서비스에 게시하는 실험은 아닙니다.

사전 검증은 모델 없이 6개 명령을 모두 실행합니다. 측정 후 모델의 JSON 답과 별개로 실험 서버 상태 및 Git 참조·upstream·HEAD·작성 파일을 검사합니다.

| 항목 | 수행·검증 범위 |
|---|---|
| `context` | 저장소·기본 브랜치·라벨 11개·활성 이슈 유형 3개 조회. 비활성 유형은 선택 목록에서 제외하고 읽기 전용 상태 확인 |
| `setup` | 카탈로그를 읽어 11개 prefix의 기존 라벨·유형 매핑을 `.tools.json`에 저장. 저장 파일 전체를 독립 기준과 비교 |
| `issue create` | 한국어 본문, 영어 `fix:` 제목, 인증 사용자, 기존 `bug` 라벨·`Bug` 유형의 이슈 1개 생성. 번호 기반 브랜치 생성·Development 연결·fetch·로컬 체크아웃 확인 |
| `issue branch` | 기존 이슈 41의 브랜치를 생성·연결·체크아웃한 뒤 같은 작업을 한 번 더 실행. 기존 연결 재조회, 브랜치 재사용, 이슈·연결 중복 생성 없음 확인 |
| `pr create` | 커밋이 준비된 번호 기반 head와 연결 이슈·원격 비교를 확인. PR 1개 생성, `Closes #41`·기존 라벨 적용과 Git 상태 보존 확인 |
| `branch cleanup` | 기존 삭제 실험과 같은 14개 상황에서 로컬 6개·원격 5개·오래된 추적 참조 2개를 후보로 판별하고 15개 대상을 보존. 실제 삭제 없이 참조·설정·worktree·작성 파일 보존 확인 |

## 한계

고정된 작은 실험의 결과입니다. 생성 실험에는 Markdown 템플릿이 없고 목록은 한 페이지이며, 충돌·부분 실패 복구·실제 GitHub 권한 및 서버 검증은 포함하지 않습니다. 캐시를 통제하지 않았으며 보정한 직접 기준은 나중에 실행됐으므로 총 토큰 차이를 금액 절감으로 해석하지 않습니다. 직접 처리용 스킬·스크립트가 이미 있다면 차이가 달라질 수 있습니다. 일반적인 명령 오류와 정상적인 브랜치 부재 확인(HTTP 404)에 사용한 토큰도 포함합니다.

기존 `issue create --dry-run`과 `branch cleanup --apply` 수치는 각각 기존 문서와 원본을 유지합니다. 새 수치와 실행 환경·프롬프트·작업이 다르므로 행 사이의 차이를 단일 옵션의 효과로 해석하지 않습니다.

## 재현

Go·Git·`gh`·Codex와 같은 모델을 사용할 수 있는 인증을 준비합니다. Linux/WSL에서 로컬 서버와 Codex 자식 실행을 허용한 환경을 사용하고, 매번 새 `/tmp` 출력 경로를 지정합니다. 첫 실행은 모델 없는 사전 검증, 두 번째 실행은 최대 36회 모델 측정입니다.

```sh
python3 scripts/benchmarks/run_command_benchmark.py --root /tmp/github-command-preflight
python3 scripts/benchmarks/run_command_benchmark.py --root /tmp/github-command-measurement --run-models
```

현재 스크립트는 Python 3 명령 이름을 안내에 포함합니다. 직접 처리만 별도로 반복하려면 다음과 같이 작업을 선택합니다.

```sh
python3 scripts/benchmarks/run_command_benchmark.py --root /tmp/github-command-direct --run-models --methods gh --tasks setup issue-create issue-branch cleanup-preview
python3 scripts/benchmarks/run_command_benchmark.py --root /tmp/github-command-pr --run-models --tasks pr-create
```

기록된 프로토콜과 완료된 실행을 보존하며 재개하려면 기존 출력 경로에 `--run-models --resume`을 사용합니다. 프로덕션 소스나 정리 실험 도우미가 변경됐다면 재개를 거부합니다. 집계 단계의 불완전 기록은 자동으로 덮어쓰지 않습니다.

출력 디렉터리에 `protocol.json`, `preflight.json`, `results.json`, `summary.json`, `report.json`과 실행별 `events.jsonl`, `answer.json`, `api-access.json`, `state-verification.json`이 남습니다. 원본 보고서는 `report.json`이며 명령별 문서는 이 보고서의 해당 task 결과를 사용합니다.
