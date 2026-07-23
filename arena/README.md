# Filler Arena

Web platform for the Filler bot game: upload a bot, it gets built in an offline
sandbox, queued against every active bot, and ranked by Elo. Every match stores a
turn-by-turn replay you can scrub through in the browser.

```
[React frontend] <-> [Go API :8080] <-> [Postgres :55432]
                          |
                          v
                   [Redis queue :56379]
                          |
                          v
              [Go worker -> Docker sandbox -> game_engine]
```

## One-time setup

```powershell
# 1. Postgres + Redis
cd arena
docker compose up -d

# 2. Match sandbox image (game engine + maps + reference robots + python3)
cd sandbox
docker build -f Dockerfile.match -t filler-arena-match .

# 3. Builder images (builds run with --network=none, so pre-pull them)
docker pull python:3.12-slim-bookworm
docker pull golang:1.22-bookworm
docker pull gcc:13-bookworm
docker pull rust:1.79-slim-bookworm

# 4. Server binaries
cd ..\server
go build -o ..\bin\api.exe .\cmd\api
go build -o ..\bin\worker.exe .\cmd\worker

# 5. Frontend
cd ..\web
npm install
npm run build
```

## Run

```powershell
cd arena
.\scripts\start.ps1     # starts api.exe + worker.exe in two windows
```

Then open http://localhost:8080 (API serves the built frontend).
For frontend development: `cd web; npm run dev` → http://localhost:5173 (proxies /api).

## How a bot enters the arena

1. `POST /api/bots` (multipart: name, owner, language, file) stores the upload
   under `data/bots/<id>/src/` and queues a **build job**.
2. The worker builds/validates it in a network-disabled container
   (python: syntax check, rust: `rustc -O` std-only, go: `go build` stdlib-only,
   c: `gcc -static`, binary: stored as-is). Failures land in the bot's
   `build_log`, visible on its page.
3. On a successful build the bot enters **audit**: the worker automatically
   plays it 5 games each (sides alternating) against `wall_e`/map00,
   `h2_d2`/map01, and `bender`/map02, requiring ≥4/5 wins on every one, plus
   an informational bonus run vs `terminator`. Clearing all three required
   gates moves it to `needs_review`; failing any of them auto-rejects it, no
   human needed. A human admin reviews the manual rubric (tests, code
   quality, visualizer bonus — none of that is automatable from a single
   uploaded file) and accepts or rejects it from `/admin/audits/:id`.
   Accepting makes the bot `active` and gives it a leaderboard entry at the
   default rating — it does **not** auto-queue any matches. From there the
   owner has to actively challenge other bots to start building a record.
4. Match jobs run `game_engine` inside `filler-arena-match` with:
   `--network=none --read-only --memory 256m --cpus 1 --pids-limit 128 --cap-drop ALL`,
   bot files streamed in via `docker cp` (no host mounts), plus a wall-clock kill
   (default 5m) enforced by the worker independently of the engine's `-t`.
5. The engine's full stdout is parsed into per-half-turn snapshots
   (`match_turns`), the final score updates `matches` and Elo `rankings` in one
   transaction.

## API

| Endpoint | Description |
|---|---|
| `POST /api/bots` | upload bot (rate-limited) |
| `GET /api/bots`, `GET /api/bots/:id` | list / detail + build log + history |
| `POST /api/matches` | queue a match `{botAId, botBId, mapId?}` (rate-limited) |
| `GET /api/matches`, `GET /api/matches/:id` | recent list / summary |
| `GET /api/matches/:id/replay` | full turn-by-turn replay data |
| `POST /api/tournaments` | start a tournament `{name, format, botIds?, mapId?}` (rate-limited) |
| `GET /api/tournaments`, `GET /api/tournaments/:id` | list / bracket + standings + matches |
| `GET /api/leaderboard` | active bots by Elo |
| `GET /api/maps` | seeded maps |

## Tournaments

Any logged-in player can start a tournament from the **Tournaments** tab:
pick a format, a map (or rotate through all of them), and either every active
bot or a hand-picked subset. Participants are seeded by current Elo.

- **Round robin** (max 16 bots): every pairing is queued immediately; the
  standings table ranks by points (win 1, draw ½), then total score
  difference, then seed. The winner is crowned when every match finishes.
- **Single elimination** (max 32 bots): a standard seeded bracket (1 vs 2
  only possible in the final); short fields give top seeds first-round byes.
  Since Filler has no tiebreak game, a drawn or errored match advances the
  better seed. The next round's matches are queued automatically as results
  come in, and the bracket renders live in the browser.

Bracket advancement is re-derived from stored match rows on every step
(idempotent), serialized by a per-tournament advisory lock, and re-synced on
worker startup — a crash can never strand a bracket half-advanced.
Tournament matches update Elo like any other match.

## Configuration (env vars, all optional)

`ARENA_DATABASE_URL`, `ARENA_REDIS_ADDR`, `ARENA_LISTEN_ADDR`, `ARENA_DATA_DIR`,
`ARENA_WEB_DIST`, `ARENA_MATCH_IMAGE`, `ARENA_ENGINE_TIMEOUT` (s, default 10),
`ARENA_MATCH_WALLCLOCK` (default 5m), `ARENA_BUILD_WALLCLOCK` (default 3m),
`ARENA_MEMORY_LIMIT` (256m), `ARENA_CPU_LIMIT` (1.0), `ARENA_PIDS_LIMIT` (128),
`ARENA_BUILD_MEMORY_LIMIT` (1g), `ARENA_BUILD_CPU_LIMIT` (2), `ARENA_BUILD_PIDS_LIMIT` (256),
`ARENA_WORKER_CONCURRENCY` (2), `ARENA_REMATCH_INTERVAL` (24h, 0 = off).

`ARENA_BUILD_CPU_LIMIT` must not exceed the host's core count (Docker rejects
`--cpus` above what's available) — set it to `1` on single-core hosts.

Defaults assume both binaries run with `arena/` as the working directory.

## Auth

Players sign in with their **Reboot01** account (`POST /api/auth/login` with
`{identifier, password}`). The API forwards the credentials to
`learn.reboot01.com/api/auth/signin` (Basic auth), exchanges the returned JWT
for the user's identity via the school's GraphQL (`login`, `firstName`,
`lastName`, `email`, `auditRatio`), and stores only that profile — never the
password or JWT. A session cookie (`arena_session`, httpOnly, 7 days) ties
uploads and challenges to the player: you can only start matches with bots you
own. `GET /api/auth/me`, `POST /api/auth/logout` complete the flow.

For local development without school credentials, start the API with
`ARENA_AUTH_DEV=1` and log in with any username + empty password. Never set
this on a deployed instance.

## Reliability

- Workers **claim jobs atomically** (`pending→building`, `queued→running`), so
  duplicate queue entries or a second worker process can never double-run a job.
- On startup the worker **recovers stuck work**: jobs left `building`/`running`
  by a crashed process are reset and everything unfinished is re-enqueued.
- A panic in one job is contained; the worker pool keeps running.
- Engine output is **capped** (64 MB stdout / 1 MB stderr) so a bot flooding
  its streams can't OOM the worker.
- Matches that die (wall-clock kill, crash) keep their **partial replay** so you
  can see where the game stopped.
- `binary` uploads are checked for an ELF header at build time; sessions are
  purged on login; the per-IP rate limiter evicts idle entries.
- `GET /api/health` reports Postgres/Redis connectivity for monitoring.
- Rating updates take a Postgres advisory lock, so they stay correct even with
  multiple worker machines sharing one database.
- API responses are gzip-compressed (map02 replays shrink ~27:1).

## Admin

Set `ARENA_ADMIN_LOGINS` (comma-separated usernames) before starting the API;
those users get an **Admin** tab after login. It shows queue depths, match/bot
status counts, 24h throughput and average match duration, and offers:
requeue errored matches, activate/deactivate bots, and delete a bot with all
its matches (reference robots are protected). Endpoints live under
`/api/admin/*` and require an admin session.

## Notes

- Reference robots (bender, h2_d2, wall_e, terminator) are seeded as `builtin`
  bots owned by `system`; they live inside the match image.
- `examples/greedy.py` is a working Python bot you can upload to test the pipeline.
- `examples/solution.rs` is a strong single-file Rust bot (heat-map strategy,
  beats all reference robots — see `examples/solution.md`); upload it as
  language `rust`.
- The game engine, maps, and reference robots live in `sandbox/` and are baked
  into the match image by `sandbox/Dockerfile.match`.
- Uploaded Rust/Go/C sources must be a single file using only the standard
  library — build containers have no network access, so dependencies (crates,
  modules) can't be fetched.
- The worker re-runs a full round robin every `ARENA_REMATCH_INTERVAL` to keep
  ratings fresh.
