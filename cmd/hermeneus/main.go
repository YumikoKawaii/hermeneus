package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/yumikokawaii/hermeneus/internal/adapter/kafka"
	"github.com/yumikokawaii/hermeneus/internal/adapter/starrocks"
	"github.com/yumikokawaii/hermeneus/internal/chserver"
	"github.com/yumikokawaii/hermeneus/internal/config"
	"github.com/yumikokawaii/hermeneus/internal/translate"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.FromEnv()

	sr, err := starrocks.New(cfg.StarRocks)
	if err != nil {
		log.Fatalf("starrocks: %v", err)
	}

	tr := translate.New(starrocks.StarRocks{})

	var w chserver.Writer
	switch cfg.Sink {
	case "", "starrocks":
		w = sr
	case "kafka":
		k, err := kafka.New(cfg.Kafka)
		if err != nil {
			log.Fatalf("kafka: %v", err)
		}
		defer k.Close()
		w = k
	default:
		log.Fatalf("unknown sink %q", cfg.Sink)
	}

	srv := chserver.New(cfg, sr, w, tr)
	log.Printf("hermeneus listening on %s (CH-native) -> StarRocks queries, %s insert sink", cfg.ListenAddr, cfg.Sink)
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		log.Fatalf("server: %v", err)
	}
}
