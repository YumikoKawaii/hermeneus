-- Minimal seed so Coroot's boot-time SELECTs return non-empty results through
-- Hermeneus. Values are illustrative; the point is exercising the read path.

INSERT INTO otel_logs_service_name_severity_text (ServiceName, SeverityText, LastSeen) VALUES
    ('checkout', 'INFO',  NOW()),
    ('checkout', 'ERROR', NOW()),
    ('cart',     'INFO',  NOW());

INSERT INTO otel_traces_service_name (ServiceName, LastSeen) VALUES
    ('checkout', NOW()),
    ('cart',     NOW());

INSERT INTO otel_traces_trace_id_ts (TraceId, Start, End) VALUES
    ('trace-aaaa', DATE_SUB(NOW(), INTERVAL 5 MINUTE), NOW());

INSERT INTO otel_traces
    (Timestamp, TraceId, SpanId, ParentSpanId, TraceState, SpanName, SpanKind, ServiceName,
     ResourceAttributes, SpanAttributes, Duration, StatusCode, StatusMessage,
     `Events.Timestamp`, `Events.Name`, `Events.Attributes`, NetSockPeerAddr)
VALUES
    (NOW(), 'trace-aaaa', 'span-0001', '', '', 'POST /checkout', 'SPAN_KIND_SERVER', 'checkout',
     map('host','a'), map('http.method','POST'), 12000000, 'STATUS_CODE_OK', '',
     [], [], [], '');

INSERT INTO profiling_profiles (ServiceName, Type, LastSeen) VALUES
    ('checkout', 'cpu', NOW()),
    ('checkout', 'memory', NOW());

INSERT INTO profiling_samples (ServiceName, Type, Start, End, Labels, StackHash, Value) VALUES
    ('checkout', 'cpu', DATE_SUB(NOW(), INTERVAL 1 MINUTE), NOW(), map('pod','checkout-1'), 111, 42);

INSERT INTO profiling_stacks (ServiceName, Hash, LastSeen, Stack) VALUES
    ('checkout', 111, NOW(), ['main', 'handler', 'checkout']);
