# telegraf-emporia

Polls the [Emporia Energy](https://www.emporiaenergy.com/) cloud API for home
energy usage and ships it to InfluxDB via Telegraf.

```
Emporia cloud (Cognito auth → devices → getChartUsage, 1s resolution, kWh)
   │  emporia-poll (run-once, prints line protocol to stdout)
   ▼
Telegraf exec input (every 1m) → influxdb_v2 output → energy bucket
   ▼
Grafana / Flux queries (kWh → Watts with ×3600000)
```

## Layout

| Path | What |
|---|---|
| `cmd/poll/` | Run-once poller for Telegraf `exec` (stdout = line protocol, logs = stderr) |
| `internal/emporia/` | Emporia API client: cached Cognito sign-in, device lookup, chart usage |
| `internal/config/` | Env-based config (`.env` for local dev, env vars for services) |
| `clients/` | AWS Cognito client |
| `main.go` | Legacy standalone daemon (kept as fallback; see below) |
| `telegraf.conf` | Telegraf pipeline: `exec` → `influxdb_v2` (no secrets — env vars only) |
| `scripts/build-poll.sh` | Cross-compile `emporia-poll` for Linux (`amd64` default, `arm64` ok) |
| `docs/` | Goals, tech stack, API/schema notes, Telegraf deploy guide |

## Quick start (local)

```sh
cp .env.example .env   # fill in values, never commit .env
go run ./cmd/poll | head -3
# datapoint,device_gid=228759 value=0.00010109762962962963 1789140710000000000
```

## Deploy (InfluxDB host)

```sh
./scripts/build-poll.sh          # -> dist/emporia-poll-linux-amd64
./scripts/build-poll.sh arm64    # Raspberry Pi etc.
```

Then on the host: install Telegraf, copy the binary to
`/usr/local/bin/emporia-poll`, place `telegraf.conf`, add the secrets file
and systemd drop-in. Full steps: [`docs/telegraf.md`](docs/telegraf.md).

Test on the host before going live:

```sh
/usr/local/bin/emporia-poll | head -3          # fetch works?
telegraf --config /etc/telegraf/emporia.conf --test | head -5   # parse works?
```

Confirm data landing:

```flux
from(bucket: "energy")
  |> range(start: -15m)
  |> filter(fn: (r) => r["_measurement"] == "datapoint")
  |> count()
```

## Configuration

| Var | Used by | Default |
|---|---|---|
| `EMPORIA_EMAIL` / `EMPORIA_PASSWORD` | poller, daemon | — (required) |
| `COGNITO_REGION` | poller, daemon | `us-east-2` |
| `COGNITO_CLIENT_ID` | poller, daemon | — (required) |
| `FETCH_WINDOW` | poller, daemon | `10m` |
| `EMPORIA_TOKEN_FILE` | poller | `$TMPDIR/emporia-token.json` |
| `POLL_INTERVAL` | daemon only | `1m` |
| `INFLUX_URL` / `INFLUX_TOKEN` / `INFLUX_ORG` / `INFLUX_BUCKET` | daemon; Telegraf output (via service env) | `http://localhost:8086` / — / — / `energy` |

The Cognito IdToken is cached on disk (`0600`) so polling doesn't sign in
every minute. The 10-minute fetch window overlaps between polls; rewrites are
idempotent in InfluxDB (same series + timestamp overwrites), which also
self-heals gaps after restarts.

## Querying: kWh → Watts

Each point is kWh consumed in one second, so average power is value × 3,600,000:

```flux
from(bucket: "energy")
  |> range(start: v.timeRangeStart, stop: v.timeRangeStop)
  |> filter(fn: (r) => r["_measurement"] == "datapoint")
  |> aggregateWindow(every: v.windowPeriod, fn: mean, createEmpty: false)
  |> map(fn: (r) => ({ r with _value: r._value * 3600000.0 }))
  |> yield(name: "watts")
```

## Notes

- Emporia timestamps must use the `Z`-suffixed form (`...T00:48:29Z`); `+0000`
  offsets get HTTP 400. See [`docs/api-and-schema.md`](docs/api-and-schema.md).
- Raw values pass through untouched, including occasional negative edge
  readings (solar backfeed or not-yet-finalized buckets) — filter in Flux if
  you want (`|> filter(fn: (r) => r._value >= 0)`).
- Requires Go 1.24+ (older toolchains emit binaries rejected by recent macOS
  `dyld` with `missing LC_UUID load command`).
- The legacy daemon (`go run .`) writes to the same bucket/measurement and
  works as an immediate fallback if Telegraf is stopped.
