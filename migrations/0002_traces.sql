-- Derived from Coroot's ClickHouse DDL (coroot/ch/client.go, `tables`).
-- CH Nested Events(...) has no StarRocks equivalent; it is decoded on the wire
-- as three parallel arrays. They are modeled here as ARRAY columns named exactly
-- `Events.Timestamp` / `Events.Name` / `Events.Attributes` (backtick-quoted) so
-- Coroot's SELECT column refs match with no rewrite. Links Nested is unused by
-- the read path and omitted.

CREATE TABLE IF NOT EXISTS otel_traces (
    Timestamp            DATETIME,
    TraceId              VARCHAR(65533),
    SpanId               VARCHAR(65533),
    ParentSpanId         VARCHAR(65533),
    TraceState           VARCHAR(65533),
    SpanName             VARCHAR(65533),
    SpanKind             VARCHAR(65533),
    ServiceName          VARCHAR(65533),
    ResourceAttributes   MAP<VARCHAR(65533), VARCHAR(65533)>,
    SpanAttributes       MAP<VARCHAR(65533), VARCHAR(65533)>,
    Duration             BIGINT,
    StatusCode           VARCHAR(65533),
    StatusMessage        VARCHAR(65533),
    `Events.Timestamp`   ARRAY<DATETIME>,
    `Events.Name`        ARRAY<VARCHAR(65533)>,
    `Events.Attributes`  ARRAY<MAP<VARCHAR(65533), VARCHAR(65533)>>,
    -- MATERIALIZED SpanAttributes['net.sock.peer.addr'] in CH; a plain column here,
    -- populated by the ingest path.
    NetSockPeerAddr      VARCHAR(65533)
)
DUPLICATE KEY (Timestamp, TraceId)
PARTITION BY date_trunc('day', Timestamp)
DISTRIBUTED BY HASH (TraceId);

-- Span duration histogram (getSpansHistogram / getTraceSpanStats MV branch).
-- CH SummingMergeTree; on StarRocks aggregate on read or maintain via async MV.
CREATE TABLE IF NOT EXISTS otel_traces_histogram (
    ServiceName     VARCHAR(65533),
    SpanName        VARCHAR(65533),
    SpanKind        VARCHAR(65533),
    Root            TINYINT,
    NetSockPeerAddr VARCHAR(65533),
    NetPeerName     VARCHAR(65533),
    NetPeerPort     VARCHAR(65533),
    Timestamp       DATETIME,
    Bucket          DOUBLE,
    Total           BIGINT,
    Failed          BIGINT
)
DUPLICATE KEY (ServiceName, SpanName, SpanKind, Root, NetSockPeerAddr, NetPeerName, NetPeerPort, Timestamp)
PARTITION BY date_trunc('day', Timestamp)
DISTRIBUTED BY HASH (ServiceName);

-- Trace-id time window (GetParentSpans / trace lookups).
CREATE TABLE IF NOT EXISTS otel_traces_trace_id_ts (
    TraceId VARCHAR(65533),
    Start   DATETIME,
    End     DATETIME
)
DUPLICATE KEY (TraceId, Start)
PARTITION BY date_trunc('day', Start)
DISTRIBUTED BY HASH (TraceId);

-- Service list (GetServicesFromTraces).
CREATE TABLE IF NOT EXISTS otel_traces_service_name (
    ServiceName VARCHAR(65533),
    LastSeen    DATETIME
)
PRIMARY KEY (ServiceName)
DISTRIBUTED BY HASH (ServiceName);
