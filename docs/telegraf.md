# Telegraf deployment (InfluxDB host)

The Go daemon is replaced by Telegraf's `exec` input running `emporia-poll`
every minute. Telegraf batches, retries, and writes to the `energy` bucket.

## 1. On the InfluxDB host

Find OS/arch first (`uname -a`), then either:

- **Package install (Debian/Ubuntu):** InfluxData apt repo, `apt install telegraf`,
  or
- **Docker:** `telegraf` image with this repo's `telegraf.conf` mounted plus
  the `emporia-poll` binary.

## 2. Build emporia-poll

From this repo (on any machine with Go 1.24+), cross-compile for the host —
defaults to Linux amd64, pass `arm64` for e.g. Raspberry Pi:

```sh
./scripts/build-poll.sh          # -> dist/emporia-poll-linux-amd64
./scripts/build-poll.sh arm64    # -> dist/emporia-poll-linux-arm64
```

Copy the matching binary to the host as `/usr/local/bin/emporia-poll`
(`chmod +x`). `dist/` is gitignored build output.

## 3. Secrets (0600, owned by the telegraf user)

`/etc/default/telegraf-emporia`, referenced from the service:

```
EMPORIA_EMAIL=
EMPORIA_PASSWORD=
COGNITO_REGION=us-east-2
COGNITO_CLIENT_ID=
FETCH_WINDOW=10m
EMPORIA_TOKEN_FILE=/var/lib/telegraf/emporia-token.json
INFLUX_TOKEN=
INFLUX_ORG=
```

`INFLUX_TOKEN` needs write access to the `energy` bucket; `INFLUX_ORG` is the
organization name or ID. `EMPORIA_TOKEN_FILE`
must be writable by the telegraf user — it caches the Cognito IdToken so we
don't sign in on every poll. (Local dev still uses `.env`; services must use
env vars because `.env` is resolved relative to the process CWD.)

systemd drop-in (`/etc/systemd/system/telegraf.service.d/emporia.conf`):

```ini
[Service]
EnvironmentFile=/etc/default/telegraf-emporia
```

Place `telegraf.conf` at `/etc/telegraf/emporia.conf` (or pass
`--config` explicitly), then `systemctl restart telegraf`.

Alternative: drop it into `/etc/telegraf/telegraf.d/emporia.conf`, which the
stock service loads automatically. Caveat: the stock
`/etc/telegraf/telegraf.conf` enables system inputs (`cpu`, `disk`, `mem`,
…) whose metrics would then also land in the `energy` bucket. Either back up
the stock file and replace it with an `[agent]`-only stub, or override
`ExecStart` in the drop-in to use only your file:

```ini
[Service]
EnvironmentFile=/etc/default/telegraf-emporia
ExecStart=
ExecStart=/usr/bin/telegraf --config /etc/telegraf/emporia.conf
```

(The bare `ExecStart=` resets the stock command; `daemon-reload` after.)

## 4. Verify

```sh
telegraf --config /etc/telegraf/emporia.conf --test   # dry run, prints metrics
```

Then check live points:

```flux
from(bucket: "energy")
  |> range(start: -15m)
  |> filter(fn: (r) => r["_measurement"] == "datapoint")
  |> count()
```

## 5. Rollback

The legacy daemon (`go run .`) writes to the same bucket and measurement,
so it can take over again immediately if Telegraf is stopped.
