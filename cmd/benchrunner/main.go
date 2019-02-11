package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/viper"

	"github.com/rickbassham/bench"
)

var (
	apiURL   string
	runID    string
	runnerID string
)

func main() {
	var err error
	defer func() {
		if err != nil {
			os.Exit(1)
		}
	}()

	viper.SetEnvPrefix("bench")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", `"`, ""))

	log.Println("starting")

	for _, e := range os.Environ() {
		log.Println(e)
	}

	runnerID = viper.GetString("runner-id")

	log.Println(runnerID)

	apiURL = viper.GetString("api-url")
	runID = viper.GetString("run-id")

	concurrency := viper.GetInt("concurrency")
	url := viper.GetString("url")
	duration := viper.GetDuration("duration")
	timeout := viper.GetDuration("timeout")

	runner := bench.NewRunner(concurrency, duration, timeout, url)

	log.Println("ready")

	time.Sleep(10 * time.Second)

	err = sendReadyToStart()
	if err != nil {
		log.Println(fmt.Sprintf("%+v", err))
		return
	}

	err = waitForStart()
	if err != nil {
		log.Println(fmt.Sprintf("%+v", err))
		return
	}

	result := runner.Run()

	err = sendResult(result)
	if err != nil {
		log.Println(fmt.Sprintf("%+v", err))
	}
}

func sendReadyToStart() error {
	resp, err := http.DefaultClient.Get(fmt.Sprintf("%s/readyToStart?runId=%s&runnerId=%s", apiURL, runID, runnerID))
	if err != nil {
		return errors.Wrap(err, "error sending ready to start")
	}

	if resp.StatusCode != http.StatusOK {
		return errors.New("non-200 status code")
	}

	return nil
}

func waitForStart() error {
	for {
		resp, err := http.DefaultClient.Get(fmt.Sprintf("%s/waitForStart?runId=%s", apiURL, runID))
		if err != nil {
			return errors.Wrap(err, "error sending wait for start")
		}

		if resp.StatusCode == http.StatusOK {
			log.Println("ready to start")
			return nil
		}

		if resp.StatusCode == http.StatusAccepted {
			log.Println("still waiting for other runners to be ready")
			time.Sleep(1 * time.Second)
			continue
		}

		return errors.New("unexpected status code")
	}
}

func sendResult(result bench.Result) error {
	var b []byte
	buf := bytes.NewBuffer(b)

	err := json.NewEncoder(buf).Encode(&result)
	if err != nil {
		return errors.Wrap(err, "error encoding result")
	}

	resp, err := http.DefaultClient.Post(fmt.Sprintf("%s/reportResult?runId=%s&runnerId=%s", apiURL, runID, runnerID), "application/json", buf)
	if err != nil {
		return errors.Wrap(err, "error sending result")
	}

	if resp.StatusCode != http.StatusOK {
		return errors.New("non-200 status code")
	}

	return nil
}
