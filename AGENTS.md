# ABox contributor rules

- Host processes never execute model-authored commands.
- Guest packages must not import `internal/tui`, `internal/provider`, or `cmd/abox`.
- Isolation claims stay Planned until Phase 0.5/18 hardware tests pass.
- Do not add host filesystem mounts, guest NICs, or TSI.
- Tests must not contain secrets.
- Five built-in tools plus discovered MCP tools. No `write_file` in milestone one.
