// Package emporia polls the Emporia Energy cloud API: Cognito sign-in
// (with a token file cache), device discovery, and chart usage.
package emporia

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/henryouly/go-cognito-sdk/clients"
	"github.com/henryouly/go-cognito-sdk/internal/config"
)

type Datapoint struct {
	Timestamp time.Time
	Value     float64
}

type deviceResponse struct {
	Devices []struct {
		DeviceGid int `json:"deviceGid"`
	} `json:"devices"`
}

type chartUsageResponse struct {
	FirstUsageInstant time.Time `json:"firstUsageInstant"`
	UsageList         []float64 `json:"usageList"`
}

type cachedToken struct {
	IdToken string    `json:"id_token"`
	Expiry  time.Time `json:"expiry"`
}

// tokenCachePath honors EMPORIA_TOKEN_FILE so service deployments can point
// it at a writable location (e.g. /var/lib/telegraf/emporia-token.json).
func tokenCachePath() string {
	if p := os.Getenv("EMPORIA_TOKEN_FILE"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "emporia-token.json")
}

func loadCachedToken() *cachedToken {
	data, err := os.ReadFile(tokenCachePath())
	if err != nil {
		return nil
	}
	var t cachedToken
	if err := json.Unmarshal(data, &t); err != nil {
		return nil
	}
	if t.IdToken == "" || time.Now().Add(time.Minute).After(t.Expiry) {
		return nil
	}
	return &t
}

func saveCachedToken(t *cachedToken) {
	data, err := json.Marshal(t)
	if err != nil {
		return
	}
	if err := os.WriteFile(tokenCachePath(), data, 0600); err != nil {
		log.Printf("warning: cannot cache Emporia token: %v", err)
	}
}

// signIn returns a valid IdToken, reusing the file cache while fresh.
func signIn(cfg *config.Config) (string, error) {
	if t := loadCachedToken(); t != nil {
		return t.IdToken, nil
	}
	cognitoClient := clients.NewCognitoClient(cfg.CognitoRegion, cfg.CognitoClientID)
	result, err := cognitoClient.SignIn(cfg.EmporiaEmail, cfg.EmporiaPassword)
	if err != nil {
		return "", fmt.Errorf("cognito sign-in: %w", err)
	}
	auth := result.AuthenticationResult
	if auth == nil || auth.IdToken == nil {
		return "", fmt.Errorf("cognito sign-in: empty auth result")
	}
	expiry := time.Now().Add(time.Hour)
	if auth.ExpiresIn != nil {
		expiry = time.Now().Add(time.Second * time.Duration(*auth.ExpiresIn))
	}
	saveCachedToken(&cachedToken{IdToken: *auth.IdToken, Expiry: expiry})
	return *auth.IdToken, nil
}

func get(h *http.Client, url, token string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authtoken", token)
	res, err := h.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d: %.200s", url, res.StatusCode, body)
	}
	return body, nil
}

// Fetch signs in (cached), takes the first device, and returns its usage
// datapoints for the configured fetch window at 1-second resolution in kWh.
// Timestamps use the Z-suffixed format Emporia requires; see docs/api-and-schema.md.
func Fetch(cfg *config.Config) (int, []Datapoint, error) {
	token, err := signIn(cfg)
	if err != nil {
		return 0, nil, err
	}
	h := &http.Client{Timeout: 15 * time.Second}

	devBody, err := get(h, "https://api.emporiaenergy.com/customers/devices", token)
	if err != nil {
		return 0, nil, fmt.Errorf("list devices: %w", err)
	}
	var dev deviceResponse
	if err := json.Unmarshal(devBody, &dev); err != nil {
		return 0, nil, fmt.Errorf("parse devices: %w", err)
	}
	if len(dev.Devices) == 0 {
		return 0, nil, fmt.Errorf("no devices found")
	}
	gid := dev.Devices[0].DeviceGid

	end := time.Now()
	start := end.Add(-cfg.FetchWindow)
	url := fmt.Sprintf("https://api.emporiaenergy.com/AppAPI?apiMethod=getChartUsage&deviceGid=%d&channel=%s&start=%s&end=%s&scale=%s&energyUnit=%s",
		gid,
		"1%2C2%2C3",
		start.UTC().Format("2006-01-02T15:04:05Z07:00"),
		end.UTC().Format("2006-01-02T15:04:05Z07:00"),
		"1S",
		"KilowattHours")
	usageBody, err := get(h, url, token)
	if err != nil {
		return 0, nil, fmt.Errorf("chart usage: %w", err)
	}
	var usage chartUsageResponse
	if err := json.Unmarshal(usageBody, &usage); err != nil {
		return 0, nil, fmt.Errorf("parse usage: %w", err)
	}

	points := make([]Datapoint, 0, len(usage.UsageList))
	ts := usage.FirstUsageInstant
	for _, v := range usage.UsageList {
		points = append(points, Datapoint{Timestamp: ts, Value: v})
		ts = ts.Add(time.Second)
	}
	return gid, points, nil
}
