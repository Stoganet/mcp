# Review instructions

## What Important means here

Reserve 🔴 Important for findings that would break behavior, leak data, or create a security vulnerability: incorrect logic, unhandled error paths that silently swallow failures, credentials or API keys exposed in tool response content, tools that accept untrusted input without validation.

Style, naming, and refactoring suggestions are 🟡 Nit at most.

## Cap the nits

Report at most five 🟡 Nits per review. If you found more, say "plus N similar items" in the summary. If all findings are nits, lead the summary with "No blocking issues."

## Do not report

- Formatting, import ordering, or lint issues — golangci-lint handles these in CI

## Always check

- Tool implementations return `mcp.NewToolResultError(...)` on failure — never panic
- No credentials, API keys, or secrets appear in tool response content
- HTTP clients used in tools have explicit timeouts set
- Tool input is validated before use
- New tools are registered in `internal/server/server.go`
