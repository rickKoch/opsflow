AGENTS

This file documents build, lint, test commands and code-style conventions for agentic coding agents working in this Go repository. It ensures consistency, reliability, and CI/CD compatibility for automated and human contributors. 

If you are an agent operating in this repo: follow these rules by default. Ask questions only if a choice materially affects public API, proto schema, or security/secrets.

--------------------------------------------------------------------------------

Build, Protobuf & Generation
---------------------------
- Build the full repo: `make build` (wrapper for `go build ./...`).
- Generate gRPC/Protobuf stubs: `make proto` (uses `protoc`, see Makefile).
- Download local `protoc` binary (no sudo): `make download-protoc`.
- Install Go protoc plugins: `make proto-tools` (installs `protoc-gen-go`, `protoc-gen-go-grpc`).

Testing: Full and Targeted
--------------------------
- Run all tests: `go test ./...`
- Run a single package: `go test ./orchestrator`
- Run a single test by name:  
  `go test ./orchestrator -run '^TestName$' -v`  
  (use `^` and `$` anchors for exact matches)
- Run a subtest: `go test ./pkg -run '^TestParent/SubtestName$' -v`
- Run with race detector: `go test ./... -race -v`
- Run with coverage: `go test ./... -coverprofile=cover.out && go tool cover -html=cover.out`
- Show only failed tests:  
  `go test -run TestName -v ./pkg | sed -n '/--- FAIL/,/PASS/p'` (or use `-json`)

Linting, Vetting and Formatting
------------------------------
- Format: `gofmt -s -w .` before commit
- Group and sort imports: `goimports -w .` (preferred) or `gofmt`; keep groups:
    1. Standard library
    2. Blank line
    3. External (third-party)
    4. Blank line
    5. Internal/package
- Vet: `go vet ./...` (run in CI)
- Static analysis: `golangci-lint run` or `staticcheck ./...` (recommended)

How to Run a Single Test (Cheat-Sheet)
--------------------------------------
- Locate package: e.g. `orchestrator` → `./orchestrator`
- Find test function: e.g. `TestOrchestratorCreate`
- Run: `go test ./orchestrator -run '^TestOrchestratorCreate$' -v`

Protobuf & Generated Artifacts
------------------------------
- Generated gRPC/Protobuf Go code is under `grpc/gen/`. Do not edit generated files!
- If `grpc/actor.proto` changes, run `make proto`, review, and commit regeneration.

Repository Layout (High-Level)
------------------------------
- `actor/`, `orchestrator/`, `workflow/`, `scheduler/`, `persistence/`, `grpc/`, `examples/` — main features
- `grpc/gen/` — generated proto

Code Style & Agentic Conventions
--------------------------------
- Always run `gofmt -s -w .`/`goimports -w .` before commits
- Import groups: stdlib, external, internal
- Keep import lines short; allow tools to wrap

Packages & Naming
-----------------
- Package names: short, lowercase, singular (use `workflow`, not `workflows`)
- Exported identifiers: Capitalized
- Unexported: camelCase
- Filenames: snake_case matching feature intent

Types & Methods
---------------
- Prefer small, focused types and interfaces
- Exported methods: doc-comment starting with type name
- Functions with cancellation: accept `context.Context` first
- Always return `(T, error)`, error last

Error Handling
--------------
- Wrap errors: `fmt.Errorf("op: %w", err)` for context
- Do not log and return same error; log only at outer boundary
- Use sentinel errors sparingly; prefer typed errors or `errors.Is`/`errors.As`
- Error messages: lower-case, no punctuation when returned

Logging
-------
- Use `logging/logger.go` for logs
- Pass context to logs where available
- Avoid `fmt.Println` in production logic, use only at main/CLI/server boundaries

Concurrency & Contexts
----------------------
- Always propagate `context.Context`; check `ctx.Done()`
- Avoid leaking goroutines: ensure goroutines exit via cancellation or on completion
- Close channels from sender; document ownership when channels cross package boundaries

Testing Conventions
-------------------
- Use table-driven tests with `t.Run("case", func(t *testing.T) {...})`
- Keep tests deterministic: inject interfaces/fakes for time/network/randomness
- Use `t.Parallel()` only for tests not mutating shared state or filesystem
- Use testing helpers for repeated setup/teardown
- Keep test helper logic in `*_test.go`; never leak helpers into production code

Documentation & Comments
------------------------
- Exported types/functions/packages must have doc comments starting with the identifier
- Internal comments: short and focused; explain "why" when non-obvious

API/Public Surface & Agentic Extensions
---------------------------------------
- Adding/changing exported functions/types is backward-compatible; document in PR
- For proto changes/gRPC interfaces: consider backward/forward compatibility & migration
- Add agentic workflows and orchestration code with extensibility and distributed tests
- Actor state should be pluggable (stateless/stateful); document in API
- Distributed registry sync via gRPC (see orchestrator and actor code)
- Favor composability, pluggability, extensible actor interface for step workflows

Files & Generated Artifacts
---------------------------
- Do not commit generated binaries/tool downloads unless for reproducibility
- `bin/protoc` is repo-local; respect automated downloads via Makefile
- Regenerate `grpc/gen` files after `.proto` changes and commit regenerated files

Security & Secrets
------------------
- Never commit secrets, API keys, or credentials
- If test requires credentials, use env vars and document required variables in code/README

Cursor / Copilot Rules
----------------------
- No `.cursor/rules/`, `.cursorrules` or `.github/copilot-instructions.md` at the time of writing
- If such files are added, agents must read and respect any additional rules

Code Review/PR Guidance for Agents
----------------------------------
- Make small, focused changes per PR; include appropriate tests
- Clearly describe changes, rationale, and required follow-ups in PR body
- If proto changed, run `make proto` and commit regenerated files
- Run all tests and static analysis before pushing

CI Recommendations
------------------
- CI should run: `gofmt -l`, `go vet ./...`, `go test ./...`, `golangci-lint run`, `make proto-check` (when proto files change)

Housekeeping & Agent Workflow
-----------------------------
- Add dependencies minimally, with justification in PR
- Keep commits atomic; never force-push main or shared branches unless coordinated
- Automated agents should document intent and rationale for changes

If You Are Blocked
------------------
- For credentials, proto schema, or runtime infra not available in CI, ask one focused question per PR/issue

Quick Reference (Commands)
--------------------------
 - `make build` — compile the repo
 - `make proto` — generate gRPC code
 - `make download-protoc` — download local protoc
 - `gofmt -s -w .` — format code
 - `goimports -w .` — fix imports
 - `go vet ./...` — static vetting
 - `go test ./...` — run tests
 - `go test ./pkg -run '^TestName$' -v` — run a single test

Appendix: Primary Files for Agentic Agents
------------------------------------------
 - `Makefile` — build & proto targets
 - `go.mod` — module & deps
 - `grpc/actor.proto` — main proto definition
 - `grpc/gen/` — generated protobuf code (do not edit)

For agentic, actor-oriented workflow, ensure:
- Pluggable actor state
- Orchestrator coordinates actor lifecycles, remote/local registry sync
- Workflow steps follow step-machine abstraction
- Distributed test coverage and extensible API/documentation
