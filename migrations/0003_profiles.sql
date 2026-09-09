-- Derived from Coroot's ClickHouse DDL (coroot/ch/client.go, `tables`).

-- Resolved stacks keyed by hash (getProfile stacks CTE: any(Stack)).
CREATE TABLE IF NOT EXISTS profiling_stacks (
    ServiceName VARCHAR(65533),
    Hash        BIGINT,
    LastSeen    DATETIME,
    Stack       ARRAY<VARCHAR(65533)>
)
PRIMARY KEY (ServiceName, Hash)
DISTRIBUTED BY HASH (Hash);

-- Raw profile samples (getProfile samples CTE: sum(Value) by StackHash).
CREATE TABLE IF NOT EXISTS profiling_samples (
    ServiceName VARCHAR(65533),
    Type        VARCHAR(65533),
    Start       DATETIME,
    End         DATETIME,
    Labels      MAP<VARCHAR(65533), VARCHAR(65533)>,
    StackHash   BIGINT,
    Value       BIGINT
)
DUPLICATE KEY (ServiceName, Type, Start, End)
PARTITION BY date_trunc('day', Start)
DISTRIBUTED BY HASH (StackHash);

-- Profile type list (GetProfileTypes).
CREATE TABLE IF NOT EXISTS profiling_profiles (
    ServiceName VARCHAR(65533),
    Type        VARCHAR(65533),
    LastSeen    DATETIME
)
PRIMARY KEY (ServiceName, Type)
DISTRIBUTED BY HASH (ServiceName);
