DROP TABLE IF EXISTS otel_logs_service_name_severity_text;
DROP TABLE IF EXISTS otel_traces_trace_id_ts;
DROP TABLE IF EXISTS otel_traces_service_name;
DROP TABLE IF EXISTS otel_traces_histogram;
DROP TABLE IF EXISTS profiling_profiles;

CREATE VIEW IF NOT EXISTS otel_logs_service_name_severity_text AS
SELECT ServiceName, SeverityText, max(Timestamp) AS LastSeen
FROM otel_logs
GROUP BY ServiceName, SeverityText;

CREATE VIEW IF NOT EXISTS otel_traces_trace_id_ts AS
SELECT TraceId, min(Timestamp) AS Start, max(Timestamp) AS End
FROM otel_traces
WHERE TraceId != ''
GROUP BY TraceId;

CREATE VIEW IF NOT EXISTS otel_traces_service_name AS
SELECT ServiceName, max(Timestamp) AS LastSeen
FROM otel_traces
GROUP BY ServiceName;

CREATE VIEW IF NOT EXISTS profiling_profiles AS
SELECT ServiceName, Type, max(End) AS LastSeen
FROM profiling_samples
GROUP BY ServiceName, Type;
