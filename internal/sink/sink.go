package sink

import "context"

type Batch struct {
	Table   string
	Columns []string
	Rows    [][]any
}

type Sink interface {
	Write(ctx context.Context, b Batch) error
}
