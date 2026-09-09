-- Derived from Coroot's ClickHouse DDL (coroot/ch/client.go, `tables`).
-- Type mapping CH -> StarRocks:
--   DateTime64(9)                     -> DATETIME   (StarRocks max precision is us; ns is truncated)
--   String / LowCardinality(String)   -> VARCHAR
--   Map(LowCardinality(String),String)-> MAP<VARCHAR,VARCHAR>
-- CODEC / INDEX / TTL / ENGINE clauses are ClickHouse-specific and dropped;
-- StarRocks handles storage, compression, and partitioning differently.

CREATE TABLE IF NOT EXISTS otel_logs (
    Timestamp          DATETIME,
    TraceId            VARCHAR(65533),
    SpanId             VARCHAR(65533),
    TraceFlags         INT,
    SeverityText       VARCHAR(65533),
    SeverityNumber     INT,
    ServiceName        VARCHAR(65533),
    Body               VARCHAR(1048576),
    ResourceAttributes MAP<VARCHAR(65533), VARCHAR(65533)>,
    LogAttributes      MAP<VARCHAR(65533), VARCHAR(65533)>
)
DUPLICATE KEY (Timestamp, TraceId)
PARTITION BY date_trunc('day', Timestamp)
DISTRIBUTED BY HASH (ServiceName);

-- Read-side lookup table (GetServicesFromLogs). Coroot fills it via an MV over
-- otel_logs; on StarRocks this is populated out-of-band (async MV or ingest job).
CREATE TABLE IF NOT EXISTS otel_logs_service_name_severity_text (
    ServiceName  VARCHAR(65533),
    SeverityText VARCHAR(65533),
    LastSeen     DATETIME
)
PRIMARY KEY (ServiceName, SeverityText)
DISTRIBUTED BY HASH (ServiceName);
