// Command emporia-poll fetches the latest Emporia usage once and prints
// InfluxDB line protocol to stdout for Telegraf's exec input plugin.
// Logs go to stderr so stdout stays parseable.
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/henryouly/go-cognito-sdk/internal/config"
	"github.com/henryouly/go-cognito-sdk/internal/emporia"
)

func main() {
	log.SetOutput(os.Stderr)

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	gid, points, err := emporia.Fetch(cfg)
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range points {
		fmt.Printf("datapoint,device_gid=%d value=%s %d\n",
			gid,
			strconv.FormatFloat(p.Value, 'f', -1, 64),
			p.Timestamp.UnixNano())
	}
}
