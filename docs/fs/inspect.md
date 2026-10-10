# `tools fs inspect`

한 번의 탐색에서 파일을 선택하고 여러 패턴의 일치 줄과 겹치는 문맥 구간을 모읍니다. 기본은 패턴·줄 범위가 없으면 파일 경로·크기만 반환합니다. 명시한 `--path` 또는 요청 파일의 경로에 `--raw`를 지정하면 선택 구간이 없어도 해당 파일의 전체 원문을 반환합니다. 목차만 요청한 `--outline`은 본문을 추가하지 않습니다.

`--pattern`은 파일 본문의 텍스트입니다. 파일명은 `--path`, 이름 패턴은 `--include`로 선택합니다. 전체 원문이 필요한 알려진 파일은 `--path FILE --raw --hash`로 바로 읽고, 작은 구간만 필요하면 처음부터 `--range`를 지정합니다. 명시하지 않은 전체 파일 탐색의 기본을 원문 출력으로 확대하지 않습니다.

```sh
tools fs inspect --root . --include '**/*.go' --pattern cache --pattern retry --context 2 --json
tools fs inspect --root . --path internal/cli/fs.go --range 10:30 --hash --json
# 조회한 원문 조각으로 바로 수정 계획을 만들 때
tools fs inspect --request requests.json --raw --hash --json
```

`--pattern`은 기본 리터럴 검색이며 `--regex`로 Go 정규식을 선택합니다. `--range START:END`는 1부터 시작하는 양끝 포함 범위입니다. 패턴과 함께 지정하면 해당 범위에서 일치 줄을 찾으며 `--context`는 일치 줄 주변도 포함합니다. `--hash`를 지정할 때만 SHA-256을 반환합니다.

각 파일의 `matches`는 일치 줄 번호, `ranges`는 `{start, lines}` 형태입니다. 겹치는 문맥은 합쳐 중복 출력하지 않습니다. 원문·줄 번호는 유지하며 내용을 요약하지 않습니다. 선택·읽기 오류는 `partial`, 출력 페이지 상한은 `page`와 `next_cursor`로 구분합니다.

텍스트 조회에는 파일의 `line_ending`(`lf`, `crlf`, `mixed`, `none`)과 `final_newline`도 반환합니다. 기본 `lines`는 줄 끝을 제외한 문자열입니다. 수정에 사용할 정확한 조각은 `--raw`로 요청합니다. 이때 구간은 `{start, text}`이며 CRLF·혼합 줄바꿈·마지막 개행을 원래 바이트대로 보존합니다. `lines`를 함께 중복 출력하지 않습니다. `text`와 원본 SHA를 계획의 `old`·`sha256`으로 사용하면 줄바꿈 복원을 위한 추가 파일 조회가 필요 없습니다.

[공통 옵션](README.md) · [실측](../benchmarks/fs/inspect.md)

파일마다 다른 구간·패턴은 `--request requests.json` 한 번으로 조회합니다. `--request -`는 표준 입력입니다. 같은 경로는 한 항목으로 묶고 `ranges`를 여러 개 지정합니다.

```json
{"version":1,"files":[
  {"path":"src/a.txt","ranges":[{"start":10,"end":30},{"start":80,"end":90}],"hash":true},
  {"path":"src/b.txt","patterns":["cache","retry"],"context":2}
]}
```

패턴이 있으면 지정 구간 안에서 일치 줄을 찾고 문맥을 포함합니다. 패턴 없이 구간만 있으면 구간의 모든 줄을 반환합니다. 겹치는 구간은 합칩니다. 파일별 `patterns`, `context`, `regex`, `hash`, `raw`를 지원하고 생략한 패턴·문맥은 공통 옵션을 따릅니다. `--request`와 `--path`·`--range`는 함께 지정하지 않습니다.

출력 상한에 도달하면 다음 줄부터 읽을 `next_cursor`를 반환합니다.

전체 JSON이 상한 안에 들어가면 커서 공간을 미리 예약하지 않고 한 번에 반환합니다. 기본 64 KiB로 시작하고 실제로 페이지가 반환될 때만 이어 읽습니다. 작은 상한을 불필요하게 강제하면 재조회 비용이 늘어납니다.

```sh
tools fs inspect --request requests.json --max-output-bytes 1024 --json
tools fs inspect --request requests.json --max-output-bytes 1024 --cursor CURSOR --json
```

`page`는 정상적인 이어 읽기이며 마지막 페이지는 `complete: true`입니다. 경로·구간·패턴·원문 옵션과 루트를 그대로 유지합니다. 커서는 선택된 파일 내용과 요청에 연결되므로 중간에 내용이 바뀌면 재시작해야 합니다. 페이지별 줄 번호는 원본 기준이며 `matches`는 해당 페이지에 나타난 일치 줄입니다. 원문 페이지도 줄 끝을 유지하므로 같은 구간의 `text`를 순서대로 붙이면 정확한 원문이 됩니다. 선택 파일·파일 크기 상한으로 누락된 자료를 커서가 자동 확장하지는 않습니다. 한 줄과 커서조차 들어가지 않는 출력 상한은 높여야 합니다.

텍스트·해시를 읽는 총 바이트는 32 MiB로 제한합니다. 이어 읽기에서도 선택 파일을 다시 확인하므로 읽기 비용은 반복될 수 있습니다. 토큰 상한을 위한 페이지 기능이며 디스크 I/O 절감을 주장하지 않습니다.

## Markdown 구조와 섹션

```sh
tools fs inspect --include "**/*.md" --outline --json
# 제목을 알고 있으면 목차를 먼저 조회하지 않아도 됩니다.
tools fs inspect --path README.md --section "빌드와 검증" --raw --hash --json
```

`--outline`은 `headings`에 원문 제목, 수준, 시작·끝 줄, 같은 제목의 `occurrence`(1부터 시작)를 반환합니다. 끝 줄은 다음 같은 수준/상위 제목 직전 또는 파일 끝이며 하위 섹션을 포함합니다. 목차만 요청하면 본문은 출력하지 않습니다.

`--section TITLE`은 정확한 제목의 전체 섹션 원문을 선택합니다. 반복 지정한 섹션과 하위 섹션의 겹치는 원문은 기존 구간처럼 합칩니다. 제목이 없거나 중복되면 `partial`로 보고하며 임의로 첫 항목을 선택하지 않습니다. 중복 제목은 요청 파일의 `occurrence`로 지정합니다.

```json
{"version":1,"files":[
  {"path":"order/README.md","outline":true,"sections":[{"title":"Build","occurrence":2}],"raw":true,"hash":true},
  {"path":"delivery/README.md","sections":[{"title":"Authentication"}],"raw":true,"hash":true}
]}
```

파일별 요청과 공통 `--outline`·`--section`을 사용할 수 있습니다. 섹션 선택은 범위·패턴·문맥 필터와 함께 사용하지 않습니다. 목차는 일반 범위·패턴 조회와 함께 요청할 수 있습니다. 원문·목차 모두 기존 출력 상한과 SHA에 연결된 커서로 이어 읽습니다.

지원하는 제목은 ATX(`#` 1–6개)와 단일 행 Setext(`===`/`---`)입니다. fenced/indented code와 인식한 HTML 블록의 제목 표기는 제외하며, 닫는 `#`는 제목에서 제거합니다. 제목의 inline markup을 렌더링하거나 anchor로 변환하지 않습니다. 인용문·목록 내부의 섹션 및 여러 행 Setext 등 전체 Markdown 렌더러의 해석은 지원 범위가 아닙니다. 그런 문서는 필요한 원문 범위로 조회합니다.

목차로 필요한 본문을 대신하지 않습니다. 전체 내용을 검토하거나 재배치하는 작업은 그 원문까지 읽고, 목차 선택에 추가 조회가 필요했다면 그 비용도 포함합니다.

여러 파일에 같은 제목·순번을 적용할 때는 요청 JSON 없이 공통 옵션을 사용합니다.

```sh
tools fs inspect --path order/README.md --path delivery/README.md --outline --section Build --section-occurrence 2 --raw --hash --json
```

`--section-occurrence N`은 모든 `--section`·선택 파일에 같은 1부터 시작하는 순번을 적용합니다. 생략하면 중복 제목을 임의로 선택하지 않습니다. 파일마다 제목·순번이 다르면 기존 `--request`의 개별 sections를 사용합니다. 설치와 명령을 이미 확인한 환경에서는 바로 조회하며, 새 환경이나 실제 실행 오류가 있을 때만 위치·도움말을 확인합니다.
