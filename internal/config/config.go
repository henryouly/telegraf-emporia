package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	EmporiaEmail    string
	EmporiaPassword string
	CognitoRegion   string
	CognitoClientID string

	InfluxURL  string
	InfluxUser string
	InfluxPass string
	InfluxDB   string

	PollInterval time.Duration
	FetchWindow  time.Duration
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// loadDotEnv parses a simple KEY=VALUE file (ignores # comments and `export ` prefix).
// Missing file is not an error so env-only setups keep working.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		v = strings.Trim(v, `"'`)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func Load() (*Config, error) {
	loadDotEnv(".env")

	cfg := &Config{
		EmporiaEmail:    os.Getenv("EMPORIA_EMAIL"),
		EmporiaPassword: os.Getenv("EMPORIA_PASSWORD"),
		CognitoRegion:   getenv("COGNITO_REGION", "us-east-2"),
		CognitoClientID: os.Getenv("COGNITO_CLIENT_ID"),

		InfluxURL:  getenv("INFLUX_URL", "http://192.168.30.3:8086"),
		InfluxUser: os.Getenv("INFLUX_USER"),
		InfluxPass: os.Getenv("INFLUX_PASSWORD"),
		InfluxDB:   getenv("INFLUX_DB", "pge"),

		PollInterval: getenvDuration("POLL_INTERVAL", time.Minute),
		FetchWindow:  getenvDuration("FETCH_WINDOW", 10*time.Minute),
	}

	var missing []string
	for k, v := range map[string]string{
		"EMPORIA_EMAIL":    cfg.EmporiaEmail,
		"EMPORIA_PASSWORD": cfg.EmporiaPassword,
		"COGNITO_CLIENT_ID": cfg.CognitoClientID,
		"INFLUX_USER":      cfg.InfluxUser,
		"INFLUX_PASSWORD":  cfg.InfluxPass,
	} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %s (see .env.example)", strings.Join(missing, ", "))
	}
	return cfg, nil
}
