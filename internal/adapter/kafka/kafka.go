package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/yumikokawaii/hermeneus/internal/config"
	"github.com/yumikokawaii/hermeneus/internal/extractor"
)

type Kafka struct {
	cl     *kgo.Client
	topics map[string]string
}

func New(cfg config.Kafka) (*Kafka, error) {
	if len(cfg.Brokers) == 0 {
		return &Kafka{topics: cfg.Topics}, nil
	}
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ProducerBatchCompression(kgo.Lz4Compression()),
	)
	if err != nil {
		return nil, err
	}
	return &Kafka{cl: cl, topics: cfg.Topics}, nil
}

func (k *Kafka) Write(target string, records []extractor.Record) error {
	if k.cl == nil {
		return fmt.Errorf("kafka: no brokers configured")
	}
	topic, ok := k.topics[target]
	if !ok {
		return fmt.Errorf("kafka: no topic mapped for target %q", target)
	}
	msgs := make([]*kgo.Record, 0, len(records))
	for i := range records {
		payload, err := encodeRecord(records[i])
		if err != nil {
			return fmt.Errorf("kafka: encode record for %s: %w", target, err)
		}
		msgs = append(msgs, &kgo.Record{Topic: topic, Value: payload})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return k.cl.ProduceSync(ctx, msgs...).FirstErr()
}

func (k *Kafka) Close() {
	if k.cl != nil {
		k.cl.Close()
	}
}

func encodeRecord(r extractor.Record) ([]byte, error) {
	if len(r.Columns) != len(r.Values) {
		return nil, fmt.Errorf("column/value count mismatch: %d vs %d", len(r.Columns), len(r.Values))
	}
	obj := make(map[string]any, len(r.Columns))
	for i, col := range r.Columns {
		obj[col] = jsonValue(r.Values[i])
	}
	return json.Marshal(obj)
}

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
