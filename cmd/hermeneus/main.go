package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/yumikokawaii/hermeneus/internal/chserver"
	"github.com/yumikokawaii/hermeneus/internal/config"
	"github.com/yumikokawaii/hermeneus/internal/relay"
	"github.com/yumikokawaii/hermeneus/internal/sink"
	"github.com/yumikokawaii/hermeneus/internal/starrocks"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.FromEnv()

	sr, err := starrocks.New(cfg.StarRocks)
	if err != nil {
		log.Fatalf("starrocks: %v", err)
	}

	var out sink.Sink
	switch cfg.Sink {
	case config.SinkStreamLoad:
		out = sr
	case config.SinkKafka:
		p, err := relay.NewPublisher(cfg.Kafka)
		if err != nil {
			log.Fatalf("relay publisher: %v", err)
		}
		defer p.Close()
		out = p
	default:
		log.Fatalf("unknown sink %q", cfg.Sink)
	}

	srv := chserver.New(cfg, sr, out)
	log.Printf("hermeneus listening on %s (CH-native) -> StarRocks, insert sink=%s", cfg.ListenAddr, cfg.Sink)
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		log.Fatalf("server: %v", err)
	}
}
