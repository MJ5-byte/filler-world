package main

import (
	"context"
	"log"
	"os/signal"
	"sync"
	"syscall"

	"filler-arena/internal/config"
	"filler-arena/internal/db"
	"filler-arena/internal/queue"
	"filler-arena/internal/worker"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
	if n, err := queue.MigrateLegacy(ctx, rdb); err != nil {
		log.Printf("legacy queue migration: %v", err)
	} else if n > 0 {
		log.Printf("migrated %d jobs from legacy queue", n)
	}

	w := &worker.Worker{Cfg: cfg, Pool: pool, RDB: rdb}
	w.Recover(ctx)
	log.Printf("worker pool: %d concurrent jobs", cfg.WorkerConcurrency)

	var wg sync.WaitGroup
	for i := 0; i < cfg.WorkerConcurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w.Run(ctx, id)
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Rematch(ctx)
	}()

	<-ctx.Done()
	log.Println("shutting down")
	wg.Wait()
}
