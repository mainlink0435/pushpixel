# AGENTS.md — PushPixel

## Branches

- `main` — production. Feature branches squash-merge into main.
- `internal` — private CI/CD infrastructure configs. CI pipeline definitions and deployment details go here, not on `main`.
- Feature branches — all development. Name like `feat/description` or `fix/description`.

## Build Targets

- Docker container image
- Windows amd64 binary
- Linux amd64 binary
- Raw standalone binaries only — no installers.
- Binaries must run as a daemon or background service.

## Git

- Never commit, amend, push, or create PRs unless explicitly asked.
- Never update git config, skip hooks, force-push, or create empty commits unless explicitly asked.
- If asked to commit: inspect `git status`, `git diff`, and `git log --oneline -10` first. Stage only intended files. Never commit secrets, `.env` files, or tokens.
- Use conventional commits: `type(scope): description` (types: feat, fix, docs, chore, refactor, test).
- Feature branches only. Squash-merge to `main`.
- If a commit fails or hooks reject it, fix the issue and create a new commit — do not amend the failed one.

## Working Together

### Before Writing Code
- Read the product brief at `docs/product-brief.md` when feature requirements are unclear.
- Read surrounding files to understand conventions, available libraries, and patterns.
- Check `go.mod` before adding dependencies — never assume a library is available.
- If a decision involves a tradeoff (library choice, API approach, architecture), present options and ask. Don't make assumptions about user preference.

### When Writing Code
- Mimic existing code style. Use the same libraries, patterns, and naming as adjacent files.
- Keep changes minimal and focused on the task. Don't refactor unrelated code.
- No magic numbers. Every tunable value comes from configuration.
- Write table-driven tests alongside implementation. Test files go next to the code they test.
- Prefer editing existing files over creating new ones.

### After Writing Code
- Run `go build ./...`, `go vet ./...`, and `go test ./...` after every change.
- If you break something, fix it before moving on. Don't leave broken builds.
- Do NOT run lint or typecheck commands that haven't been set up — ask first.

### When Investigating
- Consult official documentation via web fetch before guessing about API behavior.
- When you find something critical (API limitation, security risk, architectural dead end), report it directly. Don't bury it in a file.
- Write investigation findings to `docs/` only when they serve as reference for future work.

### Communication
- Be concise. Don't explain what you did unless asked.
- If you hit a blocker or ambiguous choice, ask. Don't guess.
- When referencing code, include file path and line number.

## Testing

- Go standard `testing` package with table-driven tests.
- Mock external dependencies (HTTP, filesystem) via Go interfaces.
- Integration tests that hit real APIs: use build tag `//go:build integration`.
- Run `go test ./...` after every change. Use `-short` during fast iteration.

## Configuration

- All operational parameters live in `config.example.yaml` and are documented there.
- The config package exposes a typed struct consumed by the rest of the app.
- No hardcoded values. If a parameter needs tuning, it goes in config.
