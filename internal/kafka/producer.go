package kafka

import (
	"context"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type Message struct {
	Topic   string
	Value   []byte
	Headers map[string]string
}

type ProducerConfig struct {
	Brokers  []string
	ClientID string
}

type Producer struct {
	cl *kgo.Client
}

func NewProducer(cfg ProducerConfig) (*Producer, error) {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.ProducerBatchCompression(kgo.Lz4Compression()),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.AllowAutoTopicCreation(),
	)
	if err != nil {
		return nil, err
	}
	return &Producer{cl: cl}, nil
}

func (p *Producer) Close() {
	p.cl.Close()
}

func (p *Producer) Produce(ctx context.Context, msgs ...Message) error {
	if len(msgs) == 0 {
		return nil
	}
	recs := make([]*kgo.Record, len(msgs))
	for i, m := range msgs {
		r := &kgo.Record{Topic: m.Topic, Value: m.Value, Timestamp: time.Now()}
		for k, v := range m.Headers {
			r.Headers = append(r.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
		}
		recs[i] = r
	}
	return p.cl.ProduceSync(ctx, recs...).FirstErr()
}
