-- Routine Loads consuming the Kafka topics that hermeneus' kafka sink publishes
-- to (internal/relay/topics.go). Topic prefix defaults to "hermeneus."; column
-- names and JSON keys mirror the *Message structs in topics.go exactly.
--
-- The Kafka broker list is environment-specific and cannot be a mysql variable,
-- so it is a literal placeholder token __KAFKA_BROKERS__ that must be substituted
-- (e.g. sed) before this file is applied. Everything else is fixed.

CREATE ROUTINE LOAD hermeneus.rl_otel_logs ON otel_logs
COLUMNS (Timestamp, TraceId, SpanId, TraceFlags, SeverityText, SeverityNumber, ServiceName, ResourceAttributes, LogAttributes, Body)
PROPERTIES (
    "format" = "json",
    "jsonpaths" = "[\"$.Timestamp\",\"$.TraceId\",\"$.SpanId\",\"$.TraceFlags\",\"$.SeverityText\",\"$.SeverityNumber\",\"$.ServiceName\",\"$.ResourceAttributes\",\"$.LogAttributes\",\"$.Body\"]",
    "desired_concurrent_number" = "3",
    "max_batch_interval" = "10"
)
FROM KAFKA (
    "kafka_broker_list" = "__KAFKA_BROKERS__",
    "kafka_topic" = "hermeneus.otel.logs",
    "property.kafka_default_offsets" = "OFFSET_BEGINNING"
);

CREATE ROUTINE LOAD hermeneus.rl_otel_traces ON otel_traces
COLUMNS (Timestamp, TraceId, SpanId, ParentSpanId, TraceState, SpanName, SpanKind, ServiceName, ResourceAttributes, SpanAttributes, Duration, StatusCode, StatusMessage, `Events.Timestamp`, `Events.Name`, `Events.Attributes`, NetSockPeerAddr)
PROPERTIES (
    "format" = "json",
    "jsonpaths" = "[\"$.Timestamp\",\"$.TraceId\",\"$.SpanId\",\"$.ParentSpanId\",\"$.TraceState\",\"$.SpanName\",\"$.SpanKind\",\"$.ServiceName\",\"$.ResourceAttributes\",\"$.SpanAttributes\",\"$.Duration\",\"$.StatusCode\",\"$.StatusMessage\",\"$.Events.Timestamp\",\"$.Events.Name\",\"$.Events.Attributes\",\"$.NetSockPeerAddr\"]",
    "desired_concurrent_number" = "3",
    "max_batch_interval" = "10"
)
FROM KAFKA (
    "kafka_broker_list" = "__KAFKA_BROKERS__",
    "kafka_topic" = "hermeneus.otel.traces",
    "property.kafka_default_offsets" = "OFFSET_BEGINNING"
);

CREATE ROUTINE LOAD hermeneus.rl_profiling_stacks ON profiling_stacks
COLUMNS (ServiceName, Hash, LastSeen, Stack)
PROPERTIES (
    "format" = "json",
    "jsonpaths" = "[\"$.ServiceName\",\"$.Hash\",\"$.LastSeen\",\"$.Stack\"]",
    "desired_concurrent_number" = "3",
    "max_batch_interval" = "10"
)
FROM KAFKA (
    "kafka_broker_list" = "__KAFKA_BROKERS__",
    "kafka_topic" = "hermeneus.profiling.stacks",
    "property.kafka_default_offsets" = "OFFSET_BEGINNING"
);

CREATE ROUTINE LOAD hermeneus.rl_profiling_samples ON profiling_samples
COLUMNS (ServiceName, Type, Start, End, Labels, StackHash, Value)
PROPERTIES (
    "format" = "json",
    "jsonpaths" = "[\"$.ServiceName\",\"$.Type\",\"$.Start\",\"$.End\",\"$.Labels\",\"$.StackHash\",\"$.Value\"]",
    "desired_concurrent_number" = "3",
    "max_batch_interval" = "10"
)
FROM KAFKA (
    "kafka_broker_list" = "__KAFKA_BROKERS__",
    "kafka_topic" = "hermeneus.profiling.samples",
    "property.kafka_default_offsets" = "OFFSET_BEGINNING"
);
