package chserver

import (
	"fmt"
	"time"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/yumikokawaii/hermeneus/internal/extractor"
)

func blockRecords(names []string, cols []proto.Column) ([]extractor.Record, error) {
	if len(cols) == 0 {
		return nil, nil
	}
	rows := cols[0].Rows()
	getters := make([]func(int) (extractor.Value, error), len(cols))
	for i, col := range cols {
		g, err := columnGetter(names[i], col)
		if err != nil {
			return nil, err
		}
		getters[i] = g
	}
	records := make([]extractor.Record, rows)
	for r := 0; r < rows; r++ {
		vals := make([]extractor.Value, len(cols))
		for c := range cols {
			v, err := getters[c](r)
			if err != nil {
				return nil, err
			}
			vals[c] = v
		}
		records[r] = extractor.Record{Columns: names, Values: vals}
	}
	return records, nil
}

func columnGetter(name string, col proto.Column) (func(int) (extractor.Value, error), error) {
	switch c := col.(type) {
	case *proto.ColStr:
		return func(i int) (extractor.Value, error) {
			return extractor.Value{Kind: extractor.KindString, V: c.Row(i)}, nil
		}, nil
	case *proto.ColLowCardinality[string]:
		return func(i int) (extractor.Value, error) {
			return extractor.Value{Kind: extractor.KindString, V: c.Row(i)}, nil
		}, nil
	case *proto.ColDateTime64:
		return func(i int) (extractor.Value, error) {
			return extractor.Value{Kind: extractor.KindTime, V: c.Row(i)}, nil
		}, nil
	case *proto.ColInt32:
		return func(i int) (extractor.Value, error) {
			return extractor.Value{Kind: extractor.KindInt, V: int64(c.Row(i))}, nil
		}, nil
	case *proto.ColInt64:
		return func(i int) (extractor.Value, error) {
			return extractor.Value{Kind: extractor.KindInt, V: c.Row(i)}, nil
		}, nil
	case *proto.ColUInt32:
		return func(i int) (extractor.Value, error) {
			return extractor.Value{Kind: extractor.KindUint, V: uint64(c.Row(i))}, nil
		}, nil
	case *proto.ColUInt64:
		return func(i int) (extractor.Value, error) {
			return extractor.Value{Kind: extractor.KindUint, V: c.Row(i)}, nil
		}, nil
	case *proto.ColMap[string, string]:
		return func(i int) (extractor.Value, error) {
			return mapValue(c.Row(i)), nil
		}, nil
	case *proto.ColArr[string]:
		return func(i int) (extractor.Value, error) {
			return strArrValue(c.Row(i)), nil
		}, nil
	case *proto.ColArr[time.Time]:
		return func(i int) (extractor.Value, error) {
			return timeArrValue(c.Row(i)), nil
		}, nil
	case *proto.ColArr[map[string]string]:
		return func(i int) (extractor.Value, error) {
			return mapArrValue(c.Row(i)), nil
		}, nil
	default:
		return nil, fmt.Errorf("insert decode: unsupported column %q type %T", name, col)
	}
}

func mapValue(m map[string]string) extractor.Value {
	out := make(map[string]extractor.Value, len(m))
	for k, v := range m {
		out[k] = extractor.Value{Kind: extractor.KindString, V: v}
	}
	return extractor.Value{Kind: extractor.KindMap, V: out}
}

func strArrValue(a []string) extractor.Value {
	out := make([]extractor.Value, len(a))
	for i, v := range a {
		out[i] = extractor.Value{Kind: extractor.KindString, V: v}
	}
	return extractor.Value{Kind: extractor.KindArray, V: out}
}

func timeArrValue(a []time.Time) extractor.Value {
	out := make([]extractor.Value, len(a))
	for i, v := range a {
		out[i] = extractor.Value{Kind: extractor.KindTime, V: v}
	}
	return extractor.Value{Kind: extractor.KindArray, V: out}
}

func mapArrValue(a []map[string]string) extractor.Value {
	out := make([]extractor.Value, len(a))
	for i, m := range a {
		out[i] = mapValue(m)
	}
	return extractor.Value{Kind: extractor.KindArray, V: out}
}
