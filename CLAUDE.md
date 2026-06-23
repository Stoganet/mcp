# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
make test     # go test -race -count=1 ./...
make lint     # golangci-lint run
make build    # compile to dist/mcp-server
make tidy     # go mod tidy

# Run a single test
go test -run TestName ./internal/config/
```

## Architecture

MCP server that exposes Stoganet ops tools to AI agents (OpenClaw/Claude) via the Model Context Protocol. Stateless — no database. Tools are the unit of work.

Transport: streamable-http. OpenClaw connects at `http://mcp-server:8080/mcp`.

**Binding:** Container listens on `:8080` (all interfaces inside Docker). No `ports:` mapping in compose — the host never sees the port. Security boundary is the Docker `internal` network.

**Request flow:**
```
OpenClaw → HTTP POST /mcp → mcp-go server → tool handler → downstream API → MCP response
```

**Adding a tool:**
1. Create `internal/tools/<domain>.go` with the tool implementation
2. Register it in `internal/server/server.go`
3. No other files change

## Go code quality (always check)

**Tool errors** must be MCP errors, not panics. Return `mcp.NewToolResultError(...)` on failure.

**Tool input validation** before calling downstream APIs. Fail fast with a descriptive MCP error.

**HTTP clients** in tools must use an explicit timeout. Never use `http.DefaultClient`.

**URL building** — use `url.JoinPath` for path segments. Never `fmt.Sprintf` — it does not encode special characters in segments.

## Security invariants (always check)

- Tools must never expose raw credentials or API keys in response content
- The server must never be bound to a public interface — `:8080` inside Docker on the `internal` network only
- All downstream API calls use parameterised/sanitised URLs
