# Astra·Luna 모델별 토큰 측정

기존 `gpt-6.1-sol` 측정에 이어 README의 9개 작업을 `gpt-6-astra`와 `gpt-6-luna`로 실행했습니다. 모델마다 직접 `gh`/`gh + git`와 `tools`를 새 세션 3회씩 비교했습니다. 모델별 54회, 전체 108회이며 high 추론과 300초 제한을 동일하게 적용했습니다. [Astra](https://developers.openai.com/api/docs/models/gpt-6-astra)와 [Luna](https://developers.openai.com/api/docs/models/gpt-6-luna)의 공식 모델 ID·지원 추론 설정을 확인했습니다.

## 결과

| 모델 | 정규화 JSON·상태 검증 | 실제 상태·행동 검증 |
|---|---:|---:|
| `gpt-6-astra` | 54/54 | 54/54 |
| `gpt-6-luna` | 45/54 | 52/54 |

실패한 실행도 평균과 원본에 포함했습니다. `*`는 실패가 포함된 작업이며 그 차이를 성공한 같은 작업의 절감 수치로 해석하지 않습니다.

### gpt-6-astra

| 작업 | 직접 처리 총 토큰 | `tools` 총 토큰 | 차이 | 직접 JSON·상태 일치 | `tools` JSON·상태 일치 |
|---|---:|---:|---:|---:|---:|
| `github context` | 30,014 | 29,772 | −0.8% | 3/3 | 3/3 |
| `github setup` | 47,546 | 30,394 | −36.1% | 3/3 | 3/3 |
| `github dependabot list / view` | 31,009 | 30,525 | −1.6% | 3/3 | 3/3 |
| `github issue create --dry-run` | 30,700 | 30,254 | −1.5% | 3/3 | 3/3 |
| `github issue create` | 97,460 | 44,469 | −54.4% | 3/3 | 3/3 |
| `github issue branch` | 122,122 | 44,603 | −63.5% | 3/3 | 3/3 |
| `github pr create` | 96,262 | 49,803 | −48.3% | 3/3 | 3/3 |
| `github branch cleanup` | 52,739 | 30,842 | −41.5% | 3/3 | 3/3 |
| `github branch cleanup --apply` | 63,238 | 30,898 | −51.1% | 3/3 | 3/3 |

| 작업 | 직접 캐시 제외 입력 | `tools` 캐시 제외 입력 | 직접 총 토큰 범위 | `tools` 총 토큰 범위 |
|---|---:|---:|---|---|
| `github context` | 15,334 | 15,193 | 30,002–30,027 | 29,769–29,777 |
| `github setup` | 21,686 | 20,369 | 47,456–47,709 | 30,373–30,408 |
| `github dependabot list / view` | 15,962 | 15,626 | 31,003–31,015 | 30,482–30,595 |
| `github issue create --dry-run` | 15,583 | 15,362 | 30,385–31,320 | 30,094–30,410 |
| `github issue create` | 24,178 | 15,278 | 96,602–98,390 | 44,462–44,476 |
| `github issue branch` | 22,931 | 11,323 | 110,326–128,407 | 44,599–44,611 |
| `github pr create` | 17,551 | 15,493 | 96,137–96,366 | 44,819–59,721 |
| `github branch cleanup` | 24,165 | 16,093 | 52,233–53,022 | 30,841–30,842 |
| `github branch cleanup --apply` | 21,732 | 16,134 | 54,733–75,300 | 30,875–30,909 |

### gpt-6-luna

| 작업 | 직접 처리 총 토큰 | `tools` 총 토큰 | 차이 | 직접 JSON·상태 일치 | `tools` JSON·상태 일치 |
|---|---:|---:|---:|---:|---:|
| `github context` | 28,710 | 28,629 | −0.3% | 3/3 | 3/3 |
| `github setup` | 45,313 | 29,158 | −35.7% | 3/3 | 3/3 |
| `github dependabot list / view` | 45,494 | 39,143 | −14.0% | 3/3 | 3/3 |
| `github issue create --dry-run` | 29,882 | 28,936 | −3.2%* | 2/3 | 3/3 |
| `github issue create` | 111,649 | 43,170 | −61.3% | 3/3 | 3/3 |
| `github issue branch` | 147,051 | 63,256 | −57.0% | 3/3 | 3/3 |
| `github pr create` | 117,882 | 48,290 | −59.0%* | 3/3 | 0/3 |
| `github branch cleanup` | 52,438 | 29,758 | −43.3%* | 1/3 | 3/3 |
| `github branch cleanup --apply` | 96,557 | 29,913 | −69.0%* | 0/3 | 3/3 |

| 작업 | 직접 캐시 제외 입력 | `tools` 캐시 제외 입력 | 직접 총 토큰 범위 | `tools` 총 토큰 범위 |
|---|---:|---:|---|---|
| `github context` | 15,374 | 10,976 | 28,623–28,775 | 28,595–28,679 |
| `github setup` | 16,407 | 14,787 | 45,119–45,538 | 29,145–29,167 |
| `github dependabot list / view` | 12,459 | 16,277 | 44,949–45,950 | 29,203–44,288 |
| `github issue create --dry-run` | 10,368 | 19,887 | 29,723–30,039 | 28,816–29,012 |
| `github issue create` | 21,008 | 20,370 | 94,043–130,028 | 28,424–72,547 |
| `github issue branch` | 13,380 | 15,475 | 108,411–176,766 | 28,676–87,915 |
| `github pr create` | 20,429 | 11,541 | 109,525–131,479 | 43,246–57,930 |
| `github branch cleanup` | 15,821 | 11,007 | 50,333–53,817 | 29,650–29,899 |
| `github branch cleanup --apply` | 30,352 | 12,058 | 78,911–116,460 | 29,868–29,960 |

### 불일치 기록

Luna에서만 9회의 정규화 JSON 불일치가 있었습니다. 세부 값과 실행 번호는 원본의 `failure_diagnostics`에 보존했습니다.

- 직접 dry-run 1회: 인증 사용자의 로그인명 대신 `@me`를 반환했습니다. 읽기 전용 상태는 유지했습니다.
- `tools` PR 3회: 반환 본문의 `Closes #41` 앞에 빈 줄이 하나 더 있었습니다. 실제 생성된 PR의 제목·본문·라벨·브랜치는 모두 맞았습니다.
- 직접 미리보기 2회: 미게시 로컬 작업과 원격 삭제 자격을 혼동하거나, 삭제된 원격 브랜치와 오래된 추적 참조를 잘못 구분하고 보존 개수도 틀렸습니다. 실제 참조는 변경하지 않았습니다.
- 직접 실제 정리 3회: 1회는 삭제 상태가 맞았지만 보존 개수를 14로 보고했습니다(기준 15). 다른 2회는 `feat/unpublished` 원격 삭제를 누락했고, 그중 1회는 `feat/squashed` 로컬 삭제와 브랜치 설정 정리도 누락했습니다. 이 2회는 실제 참조 검증도 실패했습니다.

[공유 원본 JSON](models.json)에 입력·출력·캐시 토큰, 명령, API 요청, 검증 상태, 실패 진단과 원본 이벤트 SHA를 모두 보존했습니다.

## 조건

각 작업에서 Astra 6회, Luna 6회를 실행하며 각 모델의 방식 순서는 `gh → tools → tools → gh → gh → tools`입니다. 매번 API 상태·임시 Git 저장소를 초기화하고, 두 모델에 바이트가 같은 방식별 프롬프트·스키마·기준 답을 제공합니다. 명령 묶음·`gh --jq`·로컬 스크립트를 허용하고 미리 작성한 직접 처리 자동화는 제공하지 않습니다. 설치된 `python3`와 `gh`에서 지원하는 옵션을 안내합니다.

소스가 실험 도중 바뀌지 않도록 이전에 검증한 14개 프로덕션 Go 파일과 해시가 같은 사본으로 CLI를 한 번 빌드해 모든 모델 실행에 재사용합니다. `80f71a7`의 소스에 당시 측정했던 Dependabot 도움말·라우터·커서 API·명령 구현을 포함한 사본입니다. 별도로 진행 중인 PR merge/CI 기능은 이 사본에 포함하지 않습니다. Git 작업 트리가 아닌 사본에서도 빌드되도록 `-buildvcs=false`를 사용하며, 소스·CLI 바이너리·실행기·프롬프트의 SHA-256을 기록합니다.

모든 GitHub API 요청은 로컬 실험 서버로 연결됩니다. Git fetch·체크아웃·참조 삭제는 실제 임시 Git 저장소에서 수행합니다. 실제 GitHub 서비스에 이슈·PR을 게시하거나 브랜치를 삭제하지 않습니다. 모델 없는 사전 검증에서 9개 작업의 기준 답과 상태 검증을 먼저 확인했습니다.

## 작업과 검증

| 작업 | 수행 범위 |
|---|---|
| `context` | 저장소·라벨·활성 이슈 유형을 실제로 조회하고 읽기 전용 상태 확인 |
| `setup` | 카탈로그를 읽어 11개 prefix 매핑을 저장하고 파일 전체 검증 |
| `dependabot list / view` | 커서 2페이지의 합성 경고와 경고 7 상세 조회, 패치 출처·`null` 보존 확인 |
| `issue create --dry-run` | 이슈·연결 브랜치 계획의 10개 필드와 상태 보존 확인 |
| `issue create` | 이슈 1개·Development 연결·원격 브랜치·fetch·로컬 체크아웃 확인 |
| `issue branch` | 기존 이슈의 브랜치 생성·연결·체크아웃 후 재개, 중복 생성 없음 확인 |
| `pr create` | PR 1개·접두사·한국어 본문·`Closes #41`·기존 라벨과 Git 상태 보존 확인 |
| `branch cleanup` | 후보 13개·보존 15개와 읽기 전용 상태 확인 |
| `branch cleanup --apply` | 로컬 6개·원격 5개·오래된 추적 참조 2개 삭제, 보존 대상·SHA·설정·worktree·작성 파일 확인 |

## 토큰 해석

`codex exec --json`의 `turn.completed.usage`에서 총 토큰은 `input_tokens + output_tokens`, 캐시 제외 입력은 `input_tokens - cached_input_tokens`입니다. 캐시 입력·지시문·도구 상호작용도 포함하는 에이전트 작업 전체의 값이며 CLI 자체는 모델을 호출하지 않습니다. 추론 출력은 출력 토큰에 다시 더하지 않습니다. 준비·구현·분석 비용은 제외합니다.

모든 시도를 원본에 남기고 성공률과 토큰을 함께 표시합니다. 토큰 평균은 usage가 반환된 실행의 평균입니다. 실패한 작업이 포함된 평균은 성공한 같은 작업의 절감 근거로 해석하지 않습니다. 같은 프롬프트여도 모델별 시스템 지시문·토크나이저·캐시 적중·도구 호출 방식이 달라질 수 있으므로 모델 사이의 총 토큰 차이를 추론 효율이나 금액 차이로 단정하지 않습니다.

정규화 JSON의 일치와 실제 작업 상태 검증을 구분합니다. PR 본문을 반환할 때의 빈 줄 차이처럼 실제 PR 상태는 맞지만 응답 문자열이 다른 경우도 JSON 불일치로 표시합니다. 원본의 실패 진단과 상태 검증 값을 함께 확인해야 합니다.

재개 검증기는 처음에 REST ref 조회 2회를 요구했으나, GraphQL로 원격 연결을 확인하고 Git 전환을 다시 수행하는 유효한 경로를 잘못 제외했습니다. 이미 fetch한 참조가 유효하면 두 번째 fetch는 생략할 수 있습니다. 모든 재개 실행의 API·Git 인터페이스 로그를 같은 기준으로 다시 검증했고 최초 검증 결과도 `original_collector_validation`에 보존했습니다. fetch 성공, 두 번의 전환, 기존 Development 연결 확인과 최종 상태를 검증하며 특정 REST 호출 횟수는 요구하지 않습니다. 모델 실행이나 토큰 값은 변경하지 않았습니다.

기존 Sol 값은 역사적 측정입니다. 특히 기존 Sol dry-run은 실제 다른 저장소를 읽었으며 이번 계획은 고정한 로컬 실험 데이터입니다. 기존 Sol 정리도 별도 시점의 측정이므로 세 모델 사이의 차이를 모델 변경 하나의 인과 효과로 해석하지 않습니다. Astra와 Luna는 이번 실험에서 같은 조건·소스·프롬프트를 사용합니다.

## 재현

Go·Git·`gh`·Codex와 모델 접근 권한이 있는 Linux/WSL에서 저장소 루트 기준으로 실행했습니다. [고정한 소스 사본](model-source.tar.gz)을 새 디렉터리에 풀고 새 측정 경로를 사용합니다. 첫 실행은 모델 없는 사전 검증, 두 번째는 최대 108회 모델 측정입니다.

```sh
mkdir -p /tmp/github-model-source
tar -xzf docs/benchmarks/github/model-source.tar.gz -C /tmp/github-model-source
python3 scripts/benchmarks/run_model_benchmark.py --root /tmp/github-model-preflight --source-root /tmp/github-model-source
python3 scripts/benchmarks/run_model_benchmark.py --root /tmp/github-model-measurement --source-root /tmp/github-model-source --run-models
```

한 모델이나 일부 작업은 `--models gpt-6-luna --tasks context setup`처럼 선택할 수 있습니다. 중단된 실험은 원래 `--source-root`와 출력 경로에 `--run-models --resume`을 지정해 기록된 프롬프트로 재개합니다. 이전 실행은 덮어쓰지 않습니다. 실행별 `events.jsonl`, `answer.json`, `api-access.json`, `state-verification.json`은 출력 경로의 `models/<모델>/<작업>/<실행>`에 남습니다.
