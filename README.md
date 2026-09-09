# Hermeneus

A ClickHouse-native-protocol server that translates Coroot's telemetry queries to
StarRocks. Coroot connects to Hermeneus believing it is ClickHouse; Hermeneus
speaks StarRocks (MySQL wire + Stream Load) on the other side.

Goal: run **upstream Coroot unmodified, forever**. No fork. Coroot upgrades that
do not change query shapes require zero action here.

```
  coroot ──ClickHouse native TCP (ch-go)──▶ Hermeneus ──MySQL / Stream Load──▶ StarRocks
```

## Why a shim and not a fork

A Go fork of Coroot's `clickhouse/` package is smaller work, but must be
re-applied on every upstream release. Hermeneus decouples from Coroot's binary
entirely — it is coupled only to Coroot's *query behavior*, which changes rarely.

## Coupling surface (what an upgrade could break)

Hermeneus keeps working across Coroot upgrades UNLESS a new version:

- adds a new `system.*` probe (today: `system.zookeeper`, `system.tables`)
- changes a data-query shape (new function, new column)
- bumps `ch-go` in a protocol-breaking way

Translation is AST-level, so an unrecognised query fails LOUD (logged + CH
exception) rather than silently returning wrong data — that is the upgrade tripwire.

## Reused, not reimplemented

The ClickHouse wire is NOT hand-rolled. `github.com/ClickHouse/ch-go/proto`
exposes both directions — `ClientHello.Decode`, `ServerHello.EncodeAware`,
`Query.DecodeAware`, `Block.EncodeBlock` / `DecodeRawBlock`, `ClientData.DecodeAware`,
LZ4 — so Hermeneus stays framing-compatible with whatever ch-go Coroot ships.

## Layout

- `cmd/hermeneus`        — entrypoint, config load, listen loop
- `internal/chserver`    — ClickHouse native-protocol server (handshake, packet loop, block encode)
- `internal/translate`   — CH SQL AST → StarRocks SQL rewriter (the ~15 real queries)
- `internal/starrocks`   — StarRocks client: MySQL query path + Stream Load ingest path
- `internal/system`      — canned responses for `system.*` probes and DDL passthrough
- `internal/config`      — YAML config

See `docs/DESIGN.md`.
