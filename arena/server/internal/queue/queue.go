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
	buildKey = "arena:jobs:build"
	auditKey = "arena:jobs:audit"
	matchKey = "arena:jobs:match"
)

type JobType string

const (
	JobBuild JobType = "build"
	JobAudit JobType = "audit"
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
	switch t {
	case JobBuild:
		return buildKey
	case JobAudit:
		return auditKey
	default:
		return matchKey
	}
}

func Enqueue(ctx context.Context, rdb *redis.Client, job Job) error {
	b, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return rdb.LPush(ctx, keyFor(job.Type), b).Err()
}

// Dequeue blocks up to timeout. Priority is build > audit > match: a fresh
// upload never waits behind a long match backlog, and audits (which gate a
// bot joining the ladder) still cut ahead of the general match queue.
func Dequeue(ctx context.Context, rdb *redis.Client, timeout time.Duration) (Job, bool, error) {
	res, err := rdb.BRPop(ctx, timeout, buildKey, auditKey, matchKey).Result()
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
