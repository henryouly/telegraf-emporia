package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/henryouly/go-cognito-sdk/clients"
	"github.com/henryouly/go-cognito-sdk/internal/config"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

type CustomerResponse struct {
	CustomerGid int
	Email       string
	FirstName   string
	LastName    string
	CreatedAt   time.Time
}

type DeviceResponse struct {
	Devices []DeviceData
}

type DeviceData struct {
	DeviceGid int
}

func httpRequest(h *http.Client, url string, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("authtoken", token)
	res, err := h.Do(req)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func requestDeviceGid(h *http.Client, token string) int {
	res, err := httpRequest(h, "https://api.emporiaenergy.com/customers/devices", token)
	if err != nil {
		panic(err)
	}
	if res.Body != nil {
		defer res.Body.Close()
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}

	var response DeviceResponse
	json.Unmarshal(body, &response)

	return response.Devices[0].DeviceGid
}

type ChartUsageResponse struct {
	FirstUsageInstant time.Time
	UsageList         []float64
}

type Datapoint struct {
	Timestamp time.Time
	Value     float64
}

type Result struct {
	Target     string      `json:"target"`
	Datapoints [][]float64 `json:"datapoints"`
}

func updateOneMinute(h *http.Client, token string, deviceGid int, fetchWindow time.Duration) []Datapoint {
	start := time.Now().Add(-fetchWindow)
	return requestChartUsage(h, token, deviceGid, start, time.Second, "1S", "KilowattHours")
}

func requestChartUsage(h *http.Client, token string, deviceGid int, start time.Time, interval time.Duration, scale string, unit string) []Datapoint {
	end := time.Now()
	url := fmt.Sprintf("https://api.emporiaenergy.com/AppAPI?apiMethod=getChartUsage&deviceGid=%d&channel=%s&start=%s&end=%s&scale=%s&energyUnit=%s",
		deviceGid,
		"1%2C2%2C3",
		start.UTC().Format("2006-01-02T15:04:05Z07:00"),
		end.UTC().Format("2006-01-02T15:04:05Z07:00"),
		scale,
		unit)
	res, err := httpRequest(h, url, token)
	if err != nil {
		panic(err)
	}
	if res.Body != nil {
		defer res.Body.Close()
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}

	var response ChartUsageResponse
	json.Unmarshal(body, &response)

	var datapoints []Datapoint

	timestamp := response.FirstUsageInstant
	for _, value := range response.UsageList {
		entry := Datapoint{Timestamp: timestamp, Value: value}
		datapoints = append(datapoints, entry)
		timestamp = timestamp.Add(interval)
	}

	return datapoints
}

type Target struct {
	Target string `json:"target"`
}

type Request struct {
	Targets []Target `json:"targets"`
}

type TokenInfo struct {
	IdToken string
	Expiry  time.Time
}

func signInIfRequired(old *TokenInfo, cfg *config.Config) TokenInfo {
	if old == nil || time.Now().Add(time.Minute).After(old.Expiry) {
		cognitoClient := clients.NewCognitoClient(cfg.CognitoRegion, cfg.CognitoClientID)
		result, err := cognitoClient.SignIn(cfg.EmporiaEmail, cfg.EmporiaPassword)
		if err != nil {
			panic(err)
		}
		expiryInSecond := time.Second * time.Duration(*result.AuthenticationResult.ExpiresIn)
		return TokenInfo{
			IdToken: *result.AuthenticationResult.IdToken,
			Expiry:  time.Now().Add(expiryInSecond),
		}
	}
	return *old
}

func writeToInfluxDb(writeAPI api.WriteAPIBlocking, dataPoints []Datapoint) {
	points := make([]*write.Point, 0, len(dataPoints))
	for _, dp := range dataPoints {
		points = append(points, influxdb2.NewPoint("datapoint",
			map[string]string{},
			map[string]interface{}{"value": dp.Value},
			dp.Timestamp))
	}

	if err := writeAPI.WritePoint(context.Background(), points...); err != nil {
		log.Printf("Error writing data to InfluxDB: %v\n", err)
	} else {
		fmt.Printf("Wrote %d points to InfluxDB\n", len(points))
	}
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	client := influxdb2.NewClient(cfg.InfluxURL, cfg.InfluxToken)
	defer client.Close()
	writeAPI := client.WriteAPIBlocking(cfg.InfluxOrg, cfg.InfluxBucket)

	httpClient := http.Client{
		Timeout: time.Second * 10,
	}

	tokenInfo := signInIfRequired(nil, cfg)
	deviceGid := requestDeviceGid(&httpClient, tokenInfo.IdToken)

	// Create a channel to signal the goroutine to stop
	stop := make(chan struct{})

	fetchData := func() {
		log.Println("signin if required")
		tokenInfo = signInIfRequired(&tokenInfo, cfg)
		log.Println("update one minute...")
		dataPoints := updateOneMinute(&httpClient, tokenInfo.IdToken, deviceGid, cfg.FetchWindow)
		log.Println("done...")
		if err != nil {
			log.Printf("Can't update: %v\n", err)
			return
		}
		writeToInfluxDb(writeAPI, dataPoints)
	}

	fetchData()

	go func() {
		ticker := time.NewTicker(cfg.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				fetchData()

			case <-stop:
				return
			}
		}
	}()

	fmt.Println("Daemon is running. Press Ctrl+C to stop.")
	<-make(chan os.Signal, 1)
	close(stop)
}
