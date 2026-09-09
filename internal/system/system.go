package system

// Package system provides canned answers to the ClickHouse system.* probes that
// Coroot's ch client issues on connect and during operation, so upstream Coroot
// boots against Hermeneus without a real ClickHouse behind it.
//
// Probes observed in coroot/ch/client.go and coroot/clickhouse/traces.go:
//   - EXISTS system.zookeeper                     -> 0 (standalone; disables ON CLUSTER)
//   - SELECT metadata_modification_time
//       FROM system.tables WHERE name='...'_mv'   -> drives histogram-MV freshness
//   - ClickHouse cloud detection                  -> non-cloud
//   - currentDatabase()                           -> configured db

// Probe identifies a recognised system query and its canned response.
type Probe struct {
	Match    func(sql string) bool
	Response func(db string) Result
}

// Result is a minimal in-memory result the server encodes as a CH block.
type Result struct {
	Columns []string
	CHTypes []string
	Rows    [][]any
}

// Registry returns the ordered set of known probes.
// TODO(M1): populate from the list above.
func Registry() []Probe {
	return nil
}
