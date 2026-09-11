# exord-init

[English](README.md) | [한국어](README.ko.md)

`exord-init`은 제품 기능 구현을 시작하기 전에 안전한 소프트웨어 프로젝트 기반을 준비하는 초기 단계의 모델 공통 Agent Skill이자 결정적 실행 엔진입니다.

개인의 바이브코딩(vibe-coding) 프로젝트와 팀 프로젝트를 모두 대상으로 하며 OpenAI Codex, Anthropic Claude Code, Google Gemini CLI에서 공통으로 사용하는 것을 목표로 합니다. 프레임워크부터 고르는 대신 제품 정의, 범위, 아키텍처 경계, 협업 규칙, 소스 관리, 검증과 Task 준비를 먼저 진행하고 그 결정에서 기술 설정을 도출합니다.

이 프로젝트 자체도 바이브코딩 방식으로 개발하고 있습니다. 사용자가 제품 방향과 안전 관련 결정을 주도하고 AI 코딩 에이전트가 조사, 설계, 구현, 리뷰와 테스트를 지원합니다. 여기서 바이브코딩은 검증을 생략한다는 뜻이 아닙니다. 명시적인 계약, plan-bound 승인, 자동화 테스트와 사용자 리뷰를 통해 AI가 생성한 변경을 추적하고 검증합니다.

> 개발 상태: **v0.4 개발 프로토타입**. 아직 안정 릴리스나 공식 설치 프로그램은 없습니다. 현재 엔진은 안전한 `CREATE + QUICK` 적용, 읽기 전용 `ADOPT + QUICK` 분석과 1차 복구 슬라이스를 지원합니다.

## 만드는 이유

AI를 활용해 프로젝트를 시작하면 제품 경계를 이해하기 전에 프레임워크, 중복 지침과 불필요한 파일이 먼저 늘어나는 경우가 많습니다. 모델이나 세션이 바뀌면 결정 근거와 작업 맥락이 사라지기도 합니다.

`exord-init`은 다음을 제공하는 것을 목표로 합니다.

- 기술 스택보다 제품 정의를 먼저 진행하는 초기화 순서
- `SKILL.md`를 중심으로 한 모델 공통 Agent Skill
- `AGENTS.md`의 간결한 공통 규칙과 필요한 경우에만 생성하는 제품별 연결 문서
- 관련 문서만 읽는 progressive disclosure
- Task, 검증, 담당자와 handoff를 위한 명시적인 계약
- CLI와 향후 앱 어댑터가 공유하는 결정적 계획 및 기계 판독 결과
- 자동 덮어쓰기와 파괴적 기본 동작이 없는 fail-closed 파일 처리

## 현재 지원 범위

| 영역 | 현재 상태 |
|---|---|
| `doctor`와 프로토콜 handshake | 구현됨 |
| `CREATE + QUICK` 계획 | 구현됨 |
| 계획에 결합된 사용자 승인 | 구현됨 |
| 기존 파일을 덮어쓰지 않는 원자적 적용 | 구현됨 |
| 대상 재검사, 해시, 잠금, journal과 rollback | 구현됨 |
| 영어 및 한국어 생성 문서 | 구현됨 |
| Codex, Claude Code, Gemini CLI용 문서 대상 | 구현됨 |
| 읽기 전용 `ADOPT + QUICK` inventory 및 충돌 계획 | 구현됨 |
| 복구 검사 및 별도 승인에 결합된 rollback | 구현됨 |
| `recover list` 잔여 run 발견과 복구 필요 run 존재 시 plan 차단 | 구현됨 |
| 정착된(rolled-back·finalized·미적용) run에 대한 승인 결합 `recover discard` | 구현됨 |
| 완료 전 실행되는 선언된 apply 검증(`agents-size`, `manifest-schema`, `project-required-sections`) | 구현됨 |
| 테스트 전용 fault injection(현재 2단계: 파일 publication 이후, 검증 직전) | 구현됨 |
| Upgrade 계획 및 추가 fault 단계 | 남은 v0.4 작업으로 계획됨 |
| `CUSTOM`, `REINITIALIZE`, ADOPT apply | 미구현 |
| Task 및 Git 분석 | ADOPT에서 구현됨, 생성 기능은 계획 단계 |
| commit, branch, remote와 push 자동화 | 미구현 |
| 제품별 plugin 및 extension package | 미배포 |

지원하지 않는 기능은 명시적으로 실패합니다. 어댑터가 누락된 엔진 동작을 임의의 파일 명령으로 대신해서는 안 됩니다.

## 동작 방식

```text
사용자와 Agent Skill
  -> versioned SetupIntent
  -> 결정적 엔진 검사
  -> 검토 가능한 Setup Plan + spec_sha256
  -> 해당 계획에 결합된 명시적 사용자 승인
  -> 대상 재검사 및 staged 파일 검증
  -> 기존 파일을 덮어쓰지 않는 원자적 적용
  -> 검증 및 manifest-last 완료 또는 안전한 rollback
```

대화형 Skill은 선택지를 수집하고 장단점을 설명합니다. Go 엔진만이 대상 검사, 작업 생성, 위험 수치, canonical hash, 파일 변경, journal과 결과 코드를 담당합니다.

저장소 내용, 외부 문서, issue·PR 내용과 모델 출력은 신뢰하지 않는 데이터입니다. 현재 대화 중인 사용자의 명시적인 응답만 apply를 승인할 수 있습니다.

## 요구 환경

- 엔진 빌드: Go 1.25 이상
- 선택적 schema 테스트: Python 3.10 이상
- 현재 `CREATE` 흐름에서 사용할 이미 존재하는 대상 디렉터리

안전한 apply가 traversal-resistant rooted filesystem 작업과 create-only hard-link publication을 사용하므로 Go 1.25 이상이 필요합니다.

## 소스에서 빌드

저장소를 clone하고 CLI를 빌드합니다.

```sh
git clone https://github.com/kimNarr/Exord-init.git
cd Exord-init
mkdir -p bin
go build -trimpath -o ./bin/exord-init ./engine/cmd/exord-init
./bin/exord-init doctor --json
```

PowerShell:

```powershell
git clone https://github.com/kimNarr/Exord-init.git
Set-Location Exord-init
New-Item -ItemType Directory -Force bin | Out-Null
go build -trimpath -o .\bin\exord-init.exe .\engine\cmd\exord-init
.\bin\exord-init.exe doctor --json
```

`doctor` 결과에서 protocol version `1`, `plan:create-quick`, `apply:create-quick`, `plan:adopt-quick`, `recover:inspect`, `recover:rollback`, `recover:list`, `recover:discard` capability를 확인할 수 있어야 합니다.

## 빠른 시작: `CREATE + QUICK`

### 1. 대상 폴더 준비

대상 디렉터리는 이미 존재해야 하며 비어 있거나 `.git`, `.DS_Store`, `Thumbs.db`, `desktop.ini`처럼 문서에 명시된 안전 허용 항목만 포함할 수 있습니다. 파일시스템 루트, 사용자 홈, symlink·junction 대상 또는 사용자 소유 파일이 있는 폴더는 거부합니다.

```sh
mkdir ../my-project
```

Git 저장소로 사용하려면 `plan` **전에** 대상에서 `git init`을 실행합니다. plan은 대상 fingerprint를 기록하므로 계획 후에 `.git`을 만들면 fingerprint가 바뀌어 apply가 `PLAN_STALE`로 안전하게 중단됩니다. `.git`이 없으면 manifest는 `git_mode: DOCUMENT_ONLY`로 생성됩니다.

### 2. SetupIntent 작성

대상 디렉터리 밖에 다음 내용을 `intent.json`으로 저장합니다.

```json
{
  "schema_version": 1,
  "mode": "CREATE",
  "depth": "QUICK",
  "project_summary": "팀의 릴리스 준비 상태를 관리하는 작은 프로젝트",
  "documentation_language": "ko",
  "supported_agents": ["codex", "claude", "gemini"]
}
```

`documentation_language`는 `en` 또는 `ko`를 지원합니다. `supported_agents`에는 `codex`, `claude`, `gemini` 중 하나 이상을 지정합니다.

### 3. 계획 생성 및 검토

```sh
./bin/exord-init plan \
  --intent ./intent.json \
  --target ../my-project \
  --protocol-version 1 \
  --json
```

이 명령은 프로젝트 파일을 생성하지 않습니다. 다음 정보를 포함하는 단일 JSON Result를 반환합니다.

- `run_id`, `plan_id`, canonical `spec_sha256`
- 정확한 작업 순서와 파일별 예상 해시
- 대상 identity와 fingerprint
- 검증 및 위험 요약
- `approval_request` 객체

정확한 계획과 staged bytes는 로컬 run 상태에 보존됩니다. 일반 Git 디렉터리는 `.git/exord-init/` 아래에 저장하며 Git이 없는 대상은 운영체제의 사용자 상태 경로를 사용합니다.

### 4. 정확한 계획 승인

계획 전체를 먼저 검토합니다. 결과의 `approval_request` 객체만 `approval.json`으로 복사한 다음 현재 UTC 시각을 `approved_at`에 추가합니다.

```json
{
  "schema_version": 1,
  "run_id": "<plan 결과의 run_id>",
  "plan_id": "<plan 결과의 plan_id>",
  "spec_sha256": "<plan 결과의 spec_sha256>",
  "target_identity_sha256": "<plan 결과의 target identity>",
  "approved_action": "APPLY_CREATE",
  "approved_at": "2026-09-09T03:00:00Z"
}
```

포괄적인 `--yes` 옵션은 의도적으로 제공하지 않습니다. 승인은 정확한 run, plan, spec hash, target identity, 작업 종류와 유효한 승인 시각에만 적용됩니다. 대상이나 계획이 바뀌면 apply는 안전하게 중단됩니다.

### 5. 적용

```sh
./bin/exord-init apply \
  --target ../my-project \
  --run-id <run_id> \
  --approval ./approval.json \
  --protocol-version 1 \
  --json
```

성공하면 생성된 모든 파일을 다시 해싱하고, plan에 선언된 검증(`agents-size-v1`, `manifest-schema-v1`, `project-required-sections-v1`)을 실행한 뒤에만 완료된 run bundle을 제거합니다. 검증 실패를 포함해 실패하면 현재 run이 생성했고 해시가 바뀌지 않은 파일만 rollback합니다. 완전히 복구할 수 없는 상태는 검사를 위해 보존합니다.

## 기존 프로젝트 읽기 전용 분석: `ADOPT + QUICK`

SetupIntent의 `mode`를 `ADOPT`로 지정하고 기존 프로젝트에 동일한 `plan` 명령을 실행합니다. 현재 프로토타입은 파일을 변경하거나 apply용으로 staging하지 않으며 다음 내용을 보고합니다.

- 전체 파일 목록을 노출하지 않는 bounded inventory 수치
- NFC 정규화 경로를 사용하는 content fingerprint와 명시적인 해시·검사 제한
- 로컬 Git root, branch, HEAD, dirty 상태, untracked 수, worktree 표식과 진행 중 작업
- 기존 `TASK.md` 분류와 충돌 시 기본 대체 경로인 `.exord/TASK.md`
- 알려진 지침 파일의 소유 상태
- 누락 파일의 CREATE-only 후보와 모든 충돌의 해결 선택지
- 검출 값을 출력하지 않는 비밀 가능성 수치

검사기는 `.git`, 의존성 cache, symlink와 중첩 저장소를 따라가지 않습니다. Git 검사는 optional lock과 외부 fsmonitor를 비활성화하고 네트워크 작업을 수행하지 않습니다. 비밀 가능성이 있거나 검사가 안전 한도에 도달하면 사용자가 검토할 때까지 commit 제안을 중단해야 합니다.

## 중단된 실행 복구

대상의 보존된 run bundle 목록을 확인합니다.

```sh
./bin/exord-init recover list --target ../my-project --json
```

각 항목은 `run_id`, `status`, `stage`, `started_at`과 안전한 `next_action` 하나만 보고합니다. `plan`은 새 CREATE run을 저장하기 전에 같은 검사를 수행하며, 복구 필요·apply 중단·판독 불가 run이 하나라도 있으면 아무것도 저장하지 않고 `BLOCKED`를 반환합니다. 적용된 적 없는 plan, rollback된 run, finalized bundle은 경고로만 표시합니다.

대상을 변경하기 전에 보존된 실행을 검사합니다.

```sh
./bin/exord-init recover inspect --target ../my-project --run-id <run_id> --json
```

엔진은 저장된 plan hash, journal binding, target identity, 작업 상태와 현재 파일 hash를 검증합니다. `ROLLBACK_READY`는 현재 bytes가 승인된 생성 hash와 여전히 일치하는 파일만 제거할 수 있다는 뜻입니다. 수정되거나 읽을 수 없거나 링크·유형 충돌이 있는 경로가 하나라도 있으면 자동 rollback 전체를 변경 전에 차단합니다.

Rollback에는 같은 run, plan, spec hash, target identity 및 정확한 `RECOVER_ROLLBACK` 동작에 결합된 별도 승인 문서가 필요합니다.

```sh
./bin/exord-init recover rollback \
  --target ../my-project \
  --run-id <run_id> \
  --approval ./recovery-approval.json \
  --json
```

복구는 각 파일을 제거하기 직전에 다시 검사하고 모든 단계를 journal에 기록합니다. 빈 디렉터리는 의도적으로 남기며 실패한 run bundle도 향후 명시적 처리 전까지 보존합니다.

run이 정착되면 — rollback 완료(`FAILED`), apply 완료(`FINALIZED`), 미적용(`PLANNED`/`APPROVED`) — `recover discard`가 그 bundle을 영구 제거합니다. 같은 run·plan·spec hash·target에 결합되고 정확한 `RECOVER_DISCARD` action을 가진 승인이 필요하며, `RECOVERY_REQUIRED`나 apply 중단 run은 거부하고, 실제 제거 여부를 보고합니다.

```sh
./bin/exord-init recover discard \
  --target ../my-project \
  --run-id <run_id> \
  --approval ./discard-approval.json \
  --json
```

대상 자체의 finalized 상태 cleanup은 이번 슬라이스에서 구현하지 않습니다.

## 생성되는 프로젝트 파일

현재 QUICK 흐름은 영구 최소 파일과 요청한 연결 문서만 생성합니다.

```text
.exord/manifest.json   관리 파일 baseline과 공유 generator 설정
AGENTS.md              간결한 모델 공통 프로젝트 규칙
docs/PROJECT.md        제품 요약 및 discovery 항목
CLAUDE.md              요청한 경우 생성하는 얇은 Claude Code 연결 문서
GEMINI.md              요청한 경우 생성하는 얇은 Gemini CLI 연결 문서
```

`CLAUDE.md`와 `GEMINI.md`는 전체 프로젝트 규칙을 복제하지 않고 기준 문서인 `AGENTS.md`를 참조합니다. 아키텍처 계층 문서와 `TASK.md`는 조건부 후속 결과물이며 현재 v0.4 QUICK 흐름에서는 생성하지 않습니다.

## 현재 프로토타입의 안전 보장

- QUICK은 질문 수만 줄이며 안전 검사를 줄이지 않습니다.
- 기존 사용자 파일을 덮어쓰지 않습니다.
- apply 직전에 대상 상태를 다시 검사합니다.
- Plan, 승인, target identity와 staged content hash가 모두 일치해야 합니다.
- Staging과 대상 접근에 rooted filesystem handle을 사용하고 연결된 경로 요소를 거부합니다.
- replace semantics 없이 파일을 원자적으로 생성하고 manifest를 마지막에 기록합니다.
- Rollback은 현재 run이 생성한 뒤 변경되지 않은 파일만 제거합니다.
- 실패 및 복구 필요 run 상태를 보존합니다.
- 프로젝트 run 잠금은 OS advisory lock입니다. 크래시한 run이 프로젝트를 영구 잠금 상태로 만들지 않으며, finalized된 run 이후 남은 `lock.json`은 복구 필요 상태가 아니라 경고로 처리합니다.
- 엔진은 commit, push, branch 변경, Git 이력 삭제 또는 telemetry 전송을 수행하지 않습니다.
- `REINITIALIZE`와 영구 삭제는 비활성화 상태입니다.
- ADOPT는 분석 전용이며 현재 프로토타입에서도 apply할 수 없습니다.
- 복구 rollback은 journal에 기록된 CREATE 결과 중 현재 SHA-256이 저장된 plan과 일치하는 파일만 제거하며 충돌이 있으면 변경을 차단합니다.
- 복구 승인은 최초 CREATE 승인과 분리되며 `RECOVER_ROLLBACK` 동작에 결합됩니다.

자세한 공개 경계는 [안전 계약](docs/spec/safety.md)과 [프로토콜 계약](docs/spec/protocol.md)을 참고하세요.

## Agent Skill 소스

공통 Skill 소스는 [`skill/exord-init/`](skill/exord-init/)에 있습니다.

```text
skill/exord-init/
├── SKILL.md
├── agents/openai.yaml
├── assets/templates/{en,ko}/
└── references/
```

동일한 소스에서 Codex plugin, Claude Code plugin과 Gemini CLI extension package를 생성하는 것을 목표로 합니다. 아직 제품별 설치 프로그램과 marketplace 배포 방식을 확정하지 않았으므로 안정적인 설치 명령을 제공한다고 주장하지 않습니다.

## 저장소 구조

```text
engine/cmd/exord-init/    CLI entry point
engine/internal/          Planner, apply, state, hashing과 target safety
schemas/                  Versioned JSON 계약
skill/exord-init/         설치 가능한 Agent Skill 소스
docs/spec/                공개 protocol, safety와 구현 범위
tests/                    교차 계약 및 binary schema 테스트
templates.go              내장 template loader
```

과거 기획 체크포인트, feedback 입력과 생성된 PDF는 공개 저장소에 commit하지 않고 로컬에서 관리합니다.

## 테스트

Go 테스트와 정적 분석:

```sh
go test ./...
go vet ./...
```

선택적 Python schema 테스트:

```sh
python -m pip install -r requirements-dev.txt
python -m unittest discover -s tests -p '*_test.py'
```

빌드된 실행 파일을 `EXORD_INIT_BIN`에 지정하면 실제 CREATE apply, 읽기 전용 ADOPT 및 승인형 복구 통합 테스트도 실행합니다.

```sh
EXORD_INIT_BIN=./bin/exord-init python -m unittest discover -s tests -p '*_test.py'
```

로컬에서는 Windows amd64에서 native 실행을 확인했습니다. windows/amd64, windows/arm64, linux/amd64, darwin/arm64 교차 컴파일은 성공합니다. GitHub Actions 매트릭스(`.github/workflows/ci.yml`)가 linux amd64, linux arm64, windows amd64, macOS arm64에서 `go build`, `go vet`, `go test`, trimpath CLI 빌드와 Python 계약·schema·통합 테스트를 실행합니다. release artifact 서명과 패키징은 후속 작업입니다.

## 로드맵

위험이 낮은 순서에 따른 구현 계획은 다음과 같습니다.

1. v0.1: 읽기 전용 `doctor`와 `CREATE + QUICK` 계획 — 구현됨
2. v0.2: 안전한 `CREATE + QUICK` plan-bound apply — 구현됨
3. v0.3: 읽기 전용 `ADOPT` inventory, 충돌 분석, Task 및 Git 분석 — 구현됨
4. v0.4: 복구 명령, upgrade 동작과 fault injection — 복구 검사, 승인형 rollback, `recover list` 발견과 plan 사전 차단, 정착 run에 대한 승인형 `recover discard`, 선언된 apply 검증 실행과 두 apply fault 지점은 구현됐으며 finalized 상태 cleanup, upgrade와 나머지 fault matrix는 남아 있음
5. 이후: 검증된 외부 백업과 별도 파괴적 승인을 요구하는 `REINITIALIZE`

이 로드맵은 구현 순서를 나타내며 릴리스 일정을 약속하지 않습니다.

## 라이선스

엔진, Skill, schema, 테스트와 프로젝트 문서는 [Apache License 2.0](LICENSE)을 적용합니다.

`skill/exord-init/assets/templates/`의 template 소스와 그로부터 생성된 실질적 결과물에는 [CC0 1.0](LICENSE.templates)을 적용합니다. 기존 또는 무관한 사용자 콘텐츠의 라이선스는 변경하지 않습니다.

외부 의존성 고지는 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)에서 확인할 수 있습니다.

## 기여

공개 기여 및 릴리스 절차는 아직 설계 중입니다. 관련 정책을 공개하기 전에는 큰 변경을 제안하기 전에 issue로 먼저 논의해 주세요. 안전 계약을 변경할 때는 테스트를 함께 제공해야 하며 plan binding, target 재검사, 덮어쓰기 방지와 복구 동작을 약화해서는 안 됩니다.
