package starrocks

import (
	"time"

	"github.com/yumikokawaii/hermeneus/internal/extractor"
)

func jsonValue(v extractor.Value) any {
	switch v.Kind {
	case extractor.KindNull:
		return nil
	case extractor.KindTime:
		if t, ok := v.V.(time.Time); ok {
			return t.UTC().Format("2006-01-02 15:04:05.000000000")
		}
		return nil
	case extractor.KindArray:
		elems, ok := v.V.([]extractor.Value)
		if !ok {
			return nil
		}
		out := make([]any, len(elems))
		for i, e := range elems {
			out[i] = jsonValue(e)
		}
		return out
	case extractor.KindMap:
		m, ok := v.V.(map[string]extractor.Value)
		if !ok {
			return nil
		}
		out := make(map[string]any, len(m))
		for key, e := range m {
			out[key] = jsonValue(e)
		}
		return out
	default:
		return v.V
	}
}
