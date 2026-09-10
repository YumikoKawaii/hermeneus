package relay

import (
	"context"
	"fmt"

	"github.com/yumikokawaii/hermeneus/internal/config"
	"github.com/yumikokawaii/hermeneus/internal/kafka"
	"github.com/yumikokawaii/hermeneus/internal/sink"
)

type Publisher struct {
	prefix string
	p      *kafka.Producer
}

func NewPublisher(cfg config.Kafka) (*Publisher, error) {
	p, err := kafka.NewProducer(kafka.ProducerConfig{Brokers: cfg.Brokers, ClientID: cfg.ClientID})
	if err != nil {
		return nil, err
	}
	return &Publisher{prefix: cfg.TopicPrefix, p: p}, nil
}

func (pb *Publisher) Close() {
	pb.p.Close()
}

func (pb *Publisher) Write(ctx context.Context, b sink.Batch) error {
	if len(b.Rows) == 0 {
		return nil
	}
	topic, ok := TopicFor(pb.prefix, b.Table)
	if !ok {
		return fmt.Errorf("relay: no topic for table %q", b.Table)
	}
	msgs := make([]kafka.Message, 0, len(b.Rows))
	for i := range b.Rows {
		v, err := EncodeRow(b, i)
		if err != nil {
			return err
		}
		msgs = append(msgs, kafka.Message{Topic: topic, Value: v, Headers: map[string]string{"table": b.Table}})
	}
	return pb.p.Produce(ctx, msgs...)
}
