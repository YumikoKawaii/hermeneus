package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/yumikokawaii/hermeneus/internal/chserver"
	"github.com/yumikokawaii/hermeneus/internal/config"
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

	srv := chserver.New(cfg, sr)
	log.Printf("hermeneus listening on %s (CH-native) -> StarRocks", cfg.ListenAddr)
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		log.Fatalf("server: %v", err)
	}
}
