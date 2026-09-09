# Hermeneus — Design

## 1. Position in the system

```
┌─────────┐  CH native TCP (9000/9440, LZ4)   ┌───────────────┐   MySQL :9030    ┌───────────┐
│ Coroot  │ ───────────── ch-go ────────────▶ │   Hermeneus   │ ───────────────▶ │           │
│ server  │                                    │               │                  │ StarRocks │
│ (ch-go) │ ◀──── typed column blocks ──────── │  translator   │ ◀── result set ──│           │
└─────────┘                                    │               │   Stream Load    │           │
                                               │               │ ───(HTTP :8030)─▶│           │
                                               └───────────────┘                  └───────────┘
```

Coroot's ClickHouse address (project setting / `--clickhouse`) points at Hermeneus.
Nothing else in Coroot changes.

## 2. Request lifecycle

1. **Handshake** — `chserver` accepts TCP, `ClientHello.Decode`, replies
   `ServerHello.EncodeAware` advertising a ClickHouse version Coroot's ch-go accepts.
2. **Packet loop** — read client packet code (`client_code`):
   - `Query`  → `Query.DecodeAware` → classify (see §3).
   - `Data`   → `ClientData.DecodeAware` → INSERT block → buffer for Stream Load.
   - `Ping`/`Cancel` → canned server response.
3. **Classify & route** the SQL body:
   - `system.*` / handshake probe → `internal/system` canned answer.
   - DDL (`CREATE`/`ALTER`/MV) → swallow or map (schema owned out-of-band).
   - `INSERT` → decode blocks → StarRocks Stream Load.
   - `SELECT` (the ~15 known) → `internal/translate` → StarRocks MySQL query.
4. **Encode result** — map StarRocks rows → ch `proto` typed columns
   (`Block.EncodeBlock`) in the exact column order/type Coroot's `Result.Auto()` expects.
5. Send `EndOfStream`.

## 3. Query classifier (the tripwire)

Every SELECT is matched against a registered set of known Coroot queries. Match →
translate. No match → return a ClickHouse exception packet AND log the raw SQL.
This makes an upstream query change fail loudly at exactly one spot instead of
corrupting results.

## 4. Translation map (from source audit of coroot/clickhouse/*.go)

Aggregates in play (entire set): `count, sum, countIf, min, max, any, groupArray(distinct)`.
No quantiles, no window fns, no -State/-Merge.

| CH construct | StarRocks | notes |
|---|---|---|
| `toStartOfInterval(ts, INTERVAL n SECOND)` | `from_unixtime(floor(unix/n)*n)` | 3 sites |
| `countIf(c)` | `count(if(c,1,null))` | trivial |
| `multiIf(...)` | `case when` | logs severity |
| `arrayJoin(arrayConcat(mapKeys(a),mapKeys(b)))` | `unnest` + `map_keys` | logs attr keys — lateral |
| `arrayJoin([a[@k], b[@k]])` | `unnest([...])` | logs attr values |
| `Map['k']`, `Labels['x']` | `map['k']` | syntax matches; param binding differs |
| `has(arr,x)` / `empty(arr)` | `array_contains` / `array_length=0` | profiles |
| `groupArray(distinct x)` | `array_agg(distinct x)` | traces |
| `any(x)` | `any_value(x)` | profiles stacks |
| `... GLOBAL IN (subq)` | `... IN (subq)` | drop GLOBAL |
| `Events.Timestamp` (nested) | struct/array cols | depends on SR schema |
| CTE / JOIN USING / HAVING / LIMIT | as-is | native |

## 5. Ingestion (INSERT path)

Coroot streams OTLP data in as native `ClientData` blocks. Hermeneus decodes the
columns and batches them into StarRocks **Stream Load** (HTTP, JSON or CSV) per
target table. Backpressure: bound the in-flight batch, ack the CH insert only
after Stream Load returns 200.

## 6. Type mapping (result direction)

StarRocks (MySQL proto) → ch `proto` columns. Must be exact — `Result.Auto()`
decodes by declared type. Table per query: which SR column → which `proto.Col*`
(`ColStr`, `ColUInt8`, `ColDateTime64`, `ColArr`, `ColMap`, nested).

## 7. system.* emulation (minimum set, from source)

| Probe | Answer |
|---|---|
| `EXISTS system.zookeeper` | `0` (standalone, disables ON CLUSTER path) |
| `SELECT metadata_modification_time FROM system.tables WHERE name='otel_traces_histogram_mv'` | drives MV-freshness; return a sane ts or empty |
| cloud detection (`is a ClickHouse cloud instance`) | answer non-cloud |
| `currentDatabase()` | configured db name |

## 8. What is explicitly out of scope v1

- Replicated / ON CLUSTER semantics (advertise standalone).
- Histogram materialized views — either precompute in StarRocks (async MV) or
  translate the histogram SELECT to run over base tables (source already has a
  `useTracesHistogram` fallback to base `otel_traces`).
- Arbitrary ad-hoc SQL — only Coroot's known query set is supported by contract.

## 9. Milestones

- M0  handshake + ping: Coroot connects, health check green.
- M1  system.* probes + DDL swallow: Coroot boots without error.
- M2  SELECT translate for logs (2 queries) end-to-end.
- M3  INSERT → Stream Load for one table.
- M4  traces + profiles queries.
- M5  histogram strategy.
- M6  soak against live Coroot; classifier tripwire tuning.
