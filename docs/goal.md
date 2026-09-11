# Project Goal — Energy Tracker

## 1. Problem
Track home energy usage from Emporia Energy cloud and store it locally in self-hosted InfluxDB v2 for Grafana / long-term analysis. No local reads from the Emporia Vue hardware directly; all reads go via Emporia's public cloud API.

## 2. Goal
Run a small Go daemon that:

1. Authenticates to Emporia via AWS Cognito (`us-east-2`, `USER_PASSWORD_AUTH`).
2. Discovers `deviceGid` via `GET /customers/devices`.
3. Polls `getChartUsage` every 1 minute for the last ~10 minutes at `1S` resolution, `KilowattHours` unit, channels `1,2,3`.
4. Writes resulting datapoints to InfluxDB v2 (`energy` bucket) via blocking write API.

Current implementation in `main.go:193-247` does exactly this: immediate `fetchData()` + `time.Ticker(1m)`.

## 3. Non-goals (confirmed 2026-09-08)
- Stay on Emporia Energy API (`api.emporiaenergy.com`). No new vendor.
- Target is InfluxDB v2.8.0 at `192.168.30.20:8086` (`influxdb-client-go/v2`, token auth, `energy` bucket). Migrated from v1 on 2026-09-11 after discovering the server is v2.
- Single-user, single-home use. No multi-tenant, no device control, no billing.

## 4. Functional flow
```
Cognito SignIn (clients/Cognito.go:34) → IdToken + Expiry
  → GET /customers/devices (main.go:46) → Devices[0].DeviceGid
  → loop every 60s:
      GET /AppAPI?apiMethod=getChartUsage (main.go:86)
      → ChartUsageResponse{FirstUsageInstant, UsageList}
      → expand to []Datapoint{Timestamp, Value} at 1s steps (main.go:113-117)
      → WriteAPIBlocking{org, bucket: energy} → InfluxDB v2 (main.go)
```

## 5. Success criteria
- [ ] Daemon runs 24/7, survives token expiry (refresh if `<1m` to expiry, `main.go:136`).
- [ ] No secrets in repo — all credentials via env vars / config file (see `tech-stack.md`).
- [ ] No silent data loss: HTTP/auth/Influx errors are logged with retry, not `panic`/`log.Fatal`.
- [ ] Clean shutdown on SIGINT/SIGTERM (fix current `main.go:245` bug where signal channel is never notified).
- [ ] Data queryable: `from(bucket:"energy") |> range(...) |> filter(fn:(r) => r._measurement=="datapoint")` returns written points (round-trip verified 2026-09-11).
- [ ] `go vet ./...` and `go build ./...` pass.

## 6. Known issues (from code review)
1. Hardcoded secrets: Emporia email/password, Cognito AppClient ID (`main.go:137-138`), Influx URL/user/pass/db (`main.go:151-156`).
2. Overlap duplication: 10-min window fetched every 1 min → ~9 min rewritten per tick. Needs dedup or narrower window.
3. Single device only (`Devices[0]`), channels merged, no `device_gid`/`channel` tags.
4. Fragile shutdown, `panic` on transient errors, deprecated `ioutil`, Go 1.19.
