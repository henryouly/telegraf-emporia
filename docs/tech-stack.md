# Tech Stack

## 1. Language & runtime
- **Go** — `go.mod` pins `go 1.24.0` (toolchain auto-downloads via `GOTOOLCHAIN=auto`).
  1.24 is required on macOS 26+: older toolchains (e.g. 1.22.3) emit binaries
  that `dyld` rejects with `missing LC_UUID load command`.
- Stdlib only for HTTP polling (`net/http` + `time.Ticker`), signal handling (`os/signal`), JSON (`encoding/json`).
- Replace deprecated `ioutil.ReadAll` (`main.go:54,101`) with `io.ReadAll`.

## 2. Dependencies (pinned today)
| Purpose | Package | Version | Note |
|---|---|---|---|
| Cognito auth | `github.com/aws/aws-sdk-go/service/cognitoidentityprovider` | `v1.44.289` | via `clients/Cognito.go:1-46`, `USER_PASSWORD_AUTH` flow. Deprecated upstream — consider `aws-sdk-go-v2` on next refactor, no behavior change needed now. |
| InfluxDB v2 write | `github.com/influxdata/influxdb-client-go/v2` | `v2.14.0` | `NewClient(url, token)` + `WriteAPIBlocking(org, bucket)` (`main.go`). |
| JMESPath | `github.com/jmespath/go-jmespath` | `v0.4.0` | Transitive via aws-sdk-go. |

No ORM, no web framework. Keep binary single-static for NAS/RPi deployment.

## 3. External services (confirmed)
- **Source:** Emporia Energy cloud — `https://api.emporiaenergy.com/customers/devices` + `https://api.emporiaenergy.com/AppAPI?apiMethod=getChartUsage`. Auth: Cognito `us-east-2`, header `authtoken: <IdToken>` (`main.go:37`).
- **Sink:** InfluxDB v2 (`energy` bucket, token auth; URL/org in deploy env, see `docs/telegraf.md`). Legacy daemon writes via blocking API per poll; production path is Telegraf `exec` → `influxdb_v2` output. Precision `s`.

## 4. Configuration (agreed: env vars + config file)
No secrets in code. Precedence: `env > config.yaml > default`.

Proposed vars (names only — values never committed):
```
EMPORIA_EMAIL, EMPORIA_PASSWORD
COGNITO_REGION (default us-east-2), COGNITO_CLIENT_ID
INFLUX_URL, INFLUX_TOKEN, INFLUX_ORG (name or ID), INFLUX_BUCKET (default energy)
POLL_INTERVAL (default 60s), FETCH_WINDOW (default 10m)
```

Implementation choice (minimal, per Simplicity First):
- Option A (recommended): stdlib `os.Getenv` + `godotenv` for local `.env`. Zero new abstractions.
- Option B: `Viper` if YAML + file watching is wanted later. Not needed now.

Add `.env.example` with empty placeholders, gitignore `.env`.

## 5. Proposed layout (no logic change yet)
```
cmd/tracker/main.go        # daemon, ticker, signal.Notify
internal/config/config.go  # env loader
internal/emporia/client.go # devices + chartUsage (from main.go:32-120)
internal/influx/writer.go  # batch write (from main.go:159-191)
internal/auth/cognito.go   # move clients/Cognito.go
```
Keep `clients/` working until migration lands to avoid breaking `go build`.

## 6. Tooling & ops
- `gofmt`, `go vet ./...`, `go build ./...`.
- Optional: `golangci-lint`, `Dockerfile` (multi-stage `golang:1.22-alpine`), `systemd` unit with `Restart=always`.
- Logging: stdlib `log/slog` with level + timestamp; retry with backoff (3x, 2s/4s/8s) instead of `panic` (`main.go:49,57,97,104,140`).
- Fix shutdown: `signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)` then `<-sigCh; close(stop)` — replaces broken `main.go:245`.
