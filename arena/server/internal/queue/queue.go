package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	buildKey  = "arena:jobs:build"
	matchKey  = "arena:jobs:match"
	legacyKey = "arena:jobs" // pre-split queue; drained on worker start
)

type JobType string

const (
	JobBuild JobType = "build"
	JobMatch JobType = "match"
)

type Job struct {
	Type    JobType `json:"type"`
	BotID   int64   `json:"bot_id,omitempty"`
	MatchID int64   `json:"match_id,omitempty"`
}

func Connect(ctx context.Context, addr string) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return rdb, nil
}

func keyFor(t JobType) string {
	if t == JobBuild {
		return buildKey
	}
	return matchKey
}

func Enqueue(ctx context.Context, rdb *redis.Client, job Job) error {
	b, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return rdb.LPush(ctx, keyFor(job.Type), b).Err()
}

// Dequeue blocks up to timeout. Builds take strict priority over matches so a
// fresh upload never waits behind a long match backlog.
func Dequeue(ctx context.Context, rdb *redis.Client, timeout time.Duration) (Job, bool, error) {
	res, err := rdb.BRPop(ctx, timeout, buildKey, matchKey).Result()
	if errors.Is(err, redis.Nil) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	var job Job
	if err := json.Unmarshal([]byte(res[1]), &job); err != nil {
		return Job{}, false, fmt.Errorf("malformed job %q: %w", res[1], err)
	}
	return job, true, nil
}

// Depths reports how many jobs wait in each queue.
func Depths(ctx context.Context, rdb *redis.Client) (builds, matches int64, err error) {
	builds, err = rdb.LLen(ctx, buildKey).Result()
	if err != nil {
		return 0, 0, err
	}
	matches, err = rdb.LLen(ctx, matchKey).Result()
	return builds, matches, err
}

// MigrateLegacy moves any jobs left in the old single queue into the split
// queues. Safe to call on every startup.
func MigrateLegacy(ctx context.Context, rdb *redis.Client) (int, error) {
	n := 0
	for {
		raw, err := rdb.RPop(ctx, legacyKey).Result()
		if errors.Is(err, redis.Nil) {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		var job Job
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			continue // drop malformed legacy entries
		}
		if err := Enqueue(ctx, rdb, job); err != nil {
			return n, err
		}
		n++
	}
}
