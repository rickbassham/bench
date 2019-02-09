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

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sts"
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
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	sess, err := session.NewSession()
	if err != nil {
		log.Println(err.Error())
		return
	}

	id, err := sts.New(sess).GetCallerIdentity(nil)
	if err != nil {
		log.Println(err.Error())
		return
	}

	runnerID = *id.Arn

	apiURL = viper.GetString("api-url")
	runID = viper.GetString("run-id")

	concurrency := viper.GetInt("concurrency")
	url := viper.GetString("url")
	duration := viper.GetDuration("duration")
	timeout := viper.GetDuration("timeout")

	runner := bench.NewRunner(concurrency, duration, timeout, url)

	err = sendReadyToStart()
	if err != nil {
		log.Println(err.Error())
		return
	}

	err = waitForStart()
	if err != nil {
		log.Println(err.Error())
		return
	}

	runner.Start()
	runner.Wait()

	result := runner.Result()

	err = sendResult(result)
	if err != nil {
		log.Println(err.Error())
	}
}

func sendReadyToStart() error {
	resp, err := http.DefaultClient.Get(fmt.Sprintf("%s/readyToStart?runId=%s&runnerId=%s", apiURL, runID, runnerID))
	if err != nil {
		return errors.Wrap(err, "error sending ready to start")
	}

	if resp.StatusCode != 200 {
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

		if resp.StatusCode == 200 {
			log.Println("ready to start")
			return nil
		}

		if resp.StatusCode == 202 {
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

	if resp.StatusCode != 200 {
		return errors.New("non-200 status code")
	}

	return nil
}
