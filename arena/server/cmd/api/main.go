package main

import (
	"context"
	"log"

	"filler-arena/internal/api"
	"filler-arena/internal/config"
	"filler-arena/internal/db"
	"filler-arena/internal/queue"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatal(err)
	}
	if err := db.Seed(ctx, pool); err != nil {
		log.Fatal(err)
	}

	rdb, err := queue.Connect(ctx, cfg.RedisAddr)
	if err != nil {
		log.Fatal(err)
	}

	srv := api.New(cfg, pool, rdb)
	log.Fatal(srv.LogAndServe())
}
