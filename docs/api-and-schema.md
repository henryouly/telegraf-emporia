# API & Schema

## 1. Emporia Energy API (as used today)

### 1.1 Auth — AWS Cognito
- Client: `clients.NewCognitoClient(region, appClientId)` (`clients/Cognito.go:18`), `SignIn(email, password)` with `AuthFlow=USER_PASSWORD_AUTH` (`clients/Cognito.go:36`).
- Returns `AuthenticationResult{IdToken, ExpiresIn}` → cached in `TokenInfo{IdToken, Expiry}` (`main.go:130-133`), refreshed when `now+1m > expiry` (`main.go:136`).

### 1.2 List devices
```
GET https://api.emporiaenergy.com/customers/devices
Header: authtoken: <IdToken>
```
Response mapped to (`main.go:25-30`):
```json
{ "devices": [{ "deviceGid": 12345 }] }
```
Current code takes `Devices[0]` only (`main.go:62`). Future: iterate all, tag by `deviceGid`.

### 1.3 Chart usage
Built in `main.go:86-94`:
```
GET https://api.emporiaenergy.com/AppAPI?apiMethod=getChartUsage
  &deviceGid={id}&channel=1%2C2%2C3
  &start={timestamp}&end={timestamp}
  &scale=1S&energyUnit=KilowattHours
```

**Timestamp format (verified live 2026-09-08):** must be Go's
`time.UTC().Format("2006-01-02T15:04:05Z07:00")`, which renders UTC times
with a literal `Z` suffix, e.g. `2026-09-08T00:48:29Z`. Offsets without a
colon such as `2026-09-08T00:48:05+0000` (Python `%z` output) are rejected
with `400 {"message":"Text '...' could not be parsed, unparsed text found
at index 19"}`. Prefer the `Z` form; `+00:00` is untested.
Response mapped to (`main.go:65-68`):
```json
{ "firstUsageInstant": "2026-09-08T00:00:00Z", "usageList": [0.001, 0.002] }
```
Expanded to 1-second points (`main.go:113-117`):
```go
ts := FirstUsageInstant
for _, v := range UsageList { datapoints = append(datapoints, Datapoint{ts, v}); ts = ts.Add(time.Second) }
```
Poll helper `updateOneMinute` (`main.go:80-84`): `start = now-10m`, `interval = 1s`, `scale = 1S`.

> Note: channels `1,2,3` are requested combined; per-channel split is not parsed today.

## 2. InfluxDB v1 schema

### 2.1 Current (in `main.go:159-191`)
- Client: `influx.NewHTTPClient({Addr, Username, Password})`, `NewBatchPoints({Database: pge, Precision: s})`.
- Per point: `NewPoint("datapoint", tags={}, fields={"value": float64}, timestamp)`.
- Example line protocol: `datapoint value=0.001 1725753600`

Limits: no tags → cannot filter by device/channel; measurement name generic.

### 2.2 Proposed (backward-compatible path)
- New measurement `energy_usage`, keep writing `datapoint` during migration if dashboards depend on it.
- Tags: `device_gid=<int>`, `channel=<1|2|3|combined>`.
- Field: `kwh` (float, KilowattHours per 1s bucket as returned).
- Precision `s`, retention policy TBD (e.g. `autogen` 90d raw + continuous query to 1m/1h downsamples).

Example:
```
energy_usage,device_gid=12345,channel=combined kwh=0.001 1725753600
```

Grafana:
```sql
SELECT mean("kwh") FROM "energy_usage" WHERE $timeFilter GROUP BY time(1m), "device_gid"
```

### 2.3 Overlap / dedup
- Today: 10-min window every 1 min → same `(timestamp)` rewritten 9-10x. InfluxDB overwrites same series+timestamp, so values converge but write amplification is 10x.
- Options: (a) shrink window to `70s` with 60s poll + 10s guard; (b) keep 10m for backfill resilience and accept overwrite; (c) track `lastWritten` and only write `ts > lastWritten`. Recommended: (c) + (a) combined. Document choice before changing poller.
