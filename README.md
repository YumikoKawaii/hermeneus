<div align="center">

```
    ██╗  ██╗███████╗██████╗ ███╗   ███╗███████╗███╗   ██╗███████╗██╗   ██╗███████╗
    ██║  ██║██╔════╝██╔══██╗████╗ ████║██╔════╝████╗  ██║██╔════╝██║   ██║██╔════╝
    ███████║█████╗  ██████╔╝██╔████╔██║█████╗  ██╔██╗ ██║█████╗  ██║   ██║███████╗
    ██╔══██║██╔══╝  ██╔══██╗██║╚██╔╝██║██╔══╝  ██║╚██╗██║██╔══╝  ██║   ██║╚════██║
    ██║  ██║███████╗██║  ██║██║ ╚═╝ ██║███████╗██║ ╚████║███████╗╚██████╔╝███████║
    ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝╚══════╝╚═╝  ╚═══╝╚══════╝ ╚═════╝ ╚══════╝
```

### *The Interpreter Between Two Tongues*

---

**A ClickHouse-Native Wire Server That Speaks StarRocks**

[![Author](https://img.shields.io/badge/Author-Yumiko%20Sturluson-ff69b4?style=for-the-badge)](https://github.com/yumikokawaii)
[![License](https://img.shields.io/badge/License-Private-9370DB?style=for-the-badge)]()
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=for-the-badge&logo=go&logoColor=white)]()
[![Protocol](https://img.shields.io/badge/ClickHouse%20Native-FFCC01?style=for-the-badge&logo=clickhouse&logoColor=black)]()
[![Backend](https://img.shields.io/badge/StarRocks-1E88E5?style=for-the-badge)]()

</div>

---

## About

> *"Hermes carried the words of the gods to mortals, and neither side ever knew he had changed the language."*

Welcome to **hermeneus**!

[Coroot](https://github.com/coroot/coroot) stores its logs, traces, and profiles in ClickHouse and speaks nothing
else. `hermeneus` sits where ClickHouse would be, accepts the native TCP protocol, and quietly carries every query to
**StarRocks** instead. Coroot runs **upstream, unmodified, forever** — no fork, no patch to re-apply on each release.

```
  coroot ──ClickHouse native TCP──▶ hermeneus ──MySQL wire / Stream Load / Kafka──▶ StarRocks
```

Translation is deliberate, not clever. Only the ~15 query shapes Coroot actually emits are recognised. Anything else
fails **loud** — logged and returned as a ClickHouse exception — so a Coroot upgrade that changes a query trips the
wire instead of silently returning wrong data.

## Architecture

The wire server accepts the ClickHouse native protocol and splits Coroot's traffic in two:

- **Reads** — a `SELECT` is parsed by `internal/extractor` into an engine-neutral logical IR, then rendered to
  StarRocks SQL by a **Reader** adapter and run over the MySQL wire.
- **Writes** — an `INSERT` header is parsed for its table and columns; the native data blocks are decoded row by row
  into records and handed to a **Writer** adapter.

```
  extractor ──logical IR──▶ Reader.Read(IR)      ──▶ StarRocks SQL
  extractor ──records─────▶ Writer.Write(target) ──▶ sink
```

Both seams are interfaces (`internal/adapter`), so backends are swappable:

| Adapter     | Reader | Writer                            |
|-------------|:------:|-----------------------------------|
| `starrocks` |   ✔    | `stream_load` (HTTP) or `insert` (MySQL wire) |
| `kafka`     |   —    | one JSON message per record, per-table topic  |

## Configuration

All configuration is via environment variables:

| Variable                     | Default        | Purpose                                   |
|------------------------------|----------------|-------------------------------------------|
| `HERMENEUS_LISTEN_ADDR`      | `:9000`        | CH-native listen address                  |
| `HERMENEUS_DATABASE`         | `default`      | Logical database name                     |
| `HERMENEUS_SINK`             | `starrocks`    | Insert sink: `starrocks` or `kafka`       |
| `HERMENEUS_SR_HOST`          | —              | StarRocks host                            |
| `HERMENEUS_SR_QUERY_PORT`    | `9030`         | MySQL-protocol query port                 |
| `HERMENEUS_SR_HTTP_PORT`     | `8030`         | Stream Load HTTP port                     |
| `HERMENEUS_SR_USER`          | `root`         | StarRocks user                            |
| `HERMENEUS_SR_PASSWORD`      | —              | StarRocks password                        |
| `HERMENEUS_SR_WRITE_MODE`    | `stream_load`  | StarRocks write mode: `stream_load` or `insert` |
| `HERMENEUS_KAFKA_BROKERS`    | —              | Comma-separated broker list (kafka sink)  |

## Author

<div align="center">

**~ Yumiko Sturluson ~**

*Software Engineer*

*コードよ、わがまほうとなれ*

</div>

---

<div align="center">

*~ Built with mass amounts of coffee ~*

</div>
