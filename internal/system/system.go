package system

import (
	"strings"

	"github.com/ClickHouse/ch-go/proto"
)

// Package system provides canned answers to the ClickHouse system.* probes that
// Coroot's ch client issues on connect and during operation, so upstream Coroot
// boots against Hermeneus without a real ClickHouse behind it.

type Probe struct {
	Match    func(sql string) bool
	Response func(db string) []proto.InputColumn
}

func strCol(name, val string) proto.InputColumn {
	c := new(proto.ColStr)
	c.Append(val)
	return proto.InputColumn{Name: name, Data: c}
}

func u8Col(name string, val uint8) proto.InputColumn {
	c := new(proto.ColUInt8)
	c.Append(val)
	return proto.InputColumn{Name: name, Data: c}
}

func contains(sql, sub string) bool {
	return strings.Contains(strings.ToLower(sql), strings.ToLower(sub))
}

// Registry returns the ordered set of known probes.
func Registry() []Probe {
	return []Probe{
		{
			// EXISTS system.zookeeper -> 0 (standalone; disables ON CLUSTER path).
			Match: func(sql string) bool {
				return contains(sql, "system.zookeeper")
			},
			Response: func(string) []proto.InputColumn {
				return []proto.InputColumn{u8Col("result", 0)}
			},
		},
		{
			// Cloud detection -> non-cloud.
			Match: func(sql string) bool {
				return contains(sql, "clickhouse cloud") || contains(sql, "is a clickhouse cloud")
			},
			Response: func(string) []proto.InputColumn {
				return []proto.InputColumn{u8Col("result", 0)}
			},
		},
		{
			Match: func(sql string) bool {
				return contains(sql, "system.tables") && contains(sql, "engine = 'Distributed'")
			},
			Response: func(string) []proto.InputColumn {
				return []proto.InputColumn{{Name: "cluster_name", Data: new(proto.ColStr)}}
			},
		},
		{
			Match: func(sql string) bool {
				return contains(sql, "system.clusters")
			},
			Response: func(string) []proto.InputColumn {
				return []proto.InputColumn{
					{Name: "cluster", Data: new(proto.ColStr)},
					{Name: "shard_num", Data: new(proto.ColUInt32)},
					{Name: "replica_num", Data: new(proto.ColUInt32)},
					{Name: "host_name", Data: new(proto.ColStr)},
					{Name: "port", Data: new(proto.ColUInt16)},
				}
			},
		},
		{
			Match: func(sql string) bool {
				return contains(sql, "system.parts") && contains(sql, "partition_id")
			},
			Response: func(string) []proto.InputColumn {
				return []proto.InputColumn{
					{Name: "database", Data: new(proto.ColStr)},
					{Name: "table", Data: new(proto.ColStr)},
					{Name: "partition_id", Data: new(proto.ColStr)},
				}
			},
		},
		{
			Match: func(sql string) bool {
				return contains(sql, "system.parts")
			},
			Response: func(string) []proto.InputColumn {
				return []proto.InputColumn{
					{Name: "database", Data: new(proto.ColStr)},
					{Name: "table", Data: new(proto.ColStr)},
					{Name: "bytes_on_disk", Data: new(proto.ColUInt64)},
					{Name: "data_uncompressed_bytes", Data: new(proto.ColUInt64)},
					{Name: "ttl_expr", Data: new(proto.ColStr)},
					{Name: "data_since", Data: new(proto.ColDateTime)},
				}
			},
		},
		{
			// currentDatabase() -> configured db name.
			Match: func(sql string) bool {
				return contains(sql, "currentdatabase()")
			},
			Response: func(db string) []proto.InputColumn {
				return []proto.InputColumn{strCol("currentDatabase()", db)}
			},
		},
		{
			// metadata_modification_time FROM system.tables -> empty (MV absent/fresh).
			Match: func(sql string) bool {
				return contains(sql, "system.tables") && contains(sql, "metadata_modification_time")
			},
			Response: func(string) []proto.InputColumn {
				return []proto.InputColumn{{Name: "metadata_modification_time", Data: new(proto.ColDateTime)}}
			},
		},
		{
			Match: func(sql string) bool {
				return contains(sql, "system.disks")
			},
			Response: func(string) []proto.InputColumn {
				return []proto.InputColumn{
					{Name: "name", Data: new(proto.ColStr)},
					{Name: "path", Data: new(proto.ColStr)},
					{Name: "free_space", Data: new(proto.ColUInt64)},
					{Name: "total_space", Data: new(proto.ColUInt64)},
					{Name: "type", Data: new(proto.ColStr)},
				}
			},
		},
	}
}

// Match returns the response columns for the first probe matching sql, or nil.
func Match(sql, db string) ([]proto.InputColumn, bool) {
	for _, p := range Registry() {
		if p.Match(sql) {
			return p.Response(db), true
		}
	}
	return nil, false
}

// IsDDL reports whether the statement should be swallowed (schema owned
// out-of-band): CREATE / ALTER / DROP / RENAME / SET / USE / materialized views.
func IsDDL(sql string) bool {
	s := strings.ToUpper(strings.TrimSpace(sql))
	for _, p := range []string{"CREATE", "ALTER", "DROP", "RENAME", "SET ", "USE ", "TRUNCATE", "OPTIMIZE"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
