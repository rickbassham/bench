package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/codahale/hdrhistogram"
	"github.com/docker/docker/client"
	"github.com/go-redis/redis"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/spf13/viper"

	"github.com/rickbassham/bench"
	"github.com/rickbassham/bench/container"
	"github.com/rickbassham/bench/storage"
)

type ContainerManager interface {
	StartContainer(env map[string]string) (string, error)
	GetLogs(id string) (string, error)
}

type StorageManager interface {
	SaveTask(runID string, t bench.Task) error
	GetTask(runID, taskID string) (bench.Task, error)
	SaveJob(j bench.Job) error
	GetJob(runID string) (bench.Job, error)
}

var cm ContainerManager
var sm StorageManager
var maxPerContainer int

func main() {
	var err error
	defer func() {
		if err != nil {
			os.Exit(1)
		}
	}()

	log.Println("starting")

	viper.SetEnvPrefix("bench")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	maxPerContainer = viper.GetInt("max-per-container")

	r := redis.NewClient(&redis.Options{
		Addr:     viper.GetString("redis-address"),
		Password: viper.GetString("redis-auth"),
	})

	sm = storage.NewRedis(r)

	if viper.GetString("env") == "development" {
		var c *client.Client
		c, err = client.NewClientWithOpts(client.FromEnv)
		if err != nil {
			log.Println(err.Error())
			return
		}

		c.NegotiateAPIVersion(context.Background())

		cm = container.NewDocker(c, viper.GetString("image-name"))
	} else {
		sess, err := session.NewSession()
		if err != nil {
			log.Println(err.Error())
			return
		}

		cm = container.NewAWS(
			ecs.New(sess),
			cloudwatchlogs.New(sess),
			viper.GetString("task-definition"),
			viper.GetString("log-group"),
			viper.GetString("cluster"),
			viper.GetStringSlice("subnets"),
			viper.GetStringSlice("security-groups"),
			viper.GetBool("public-ip"))
	}

	http.HandleFunc("/health", health)
	http.HandleFunc("/start", start)
	http.HandleFunc("/readyToStart", readyToStart)
	http.HandleFunc("/waitForStart", waitForStart)
	http.HandleFunc("/reportResult", reportResult)
	http.HandleFunc("/result", result)
	http.HandleFunc("/logs", logs)
	http.HandleFunc("/tasks", tasks)

	err = http.ListenAndServe(":3000", nil)
	if err != nil {
		log.Println(err.Error())
		return
	}
}

func health(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

func writeErr(w http.ResponseWriter, err error) {
	w.WriteHeader(500)
	w.Write([]byte(fmt.Sprintf("%+v", err)))
	log.Println(fmt.Sprintf("%+v", err))
	return
}

func start(w http.ResponseWriter, r *http.Request) {
	log.Println("info")

	q := r.URL.Query()

	concurrency64, err := strconv.ParseInt(q.Get("concurrency"), 10, 64)
	if err != nil || concurrency64 <= 0 {
		w.WriteHeader(400)
		w.Write([]byte("concurrency must be > 0"))
		return
	}

	concurrency := int(concurrency64)

	duration, err := time.ParseDuration(q.Get("duration"))
	if err != nil || duration <= 0 {
		w.WriteHeader(400)
		w.Write([]byte("duration must be > 0"))
		return
	}

	timeout, err := time.ParseDuration(q.Get("timeout"))
	if err != nil || timeout <= 0 || timeout > 2*time.Second {
		w.WriteHeader(400)
		w.Write([]byte("timeout must be > 0 and <= 2ms"))
		return
	}

	u, err := url.Parse(q.Get("url"))
	if err != nil || !u.IsAbs() {
		w.WriteHeader(400)
		w.Write([]byte("url must be a valid absolute url"))
		return
	}

	runID := uuid.New().String()

	j := bench.Job{
		Concurrency: concurrency,
		Duration:    duration,
		RequestTime: time.Now(),
		RunID:       runID,
		Timeout:     timeout,
		URL:         u.String(),
	}

	if r.Body != nil {
		err = json.NewDecoder(r.Body).Decode(&j.MetaData)
		if err != nil {
			writeErr(w, errors.Wrap(err, "error decoding body"))
			return
		}
	}

	for i := concurrency; i > 0; i -= maxPerContainer {
		c := maxPerContainer
		if i < c {
			c = i
		}

		runnerID := uuid.New().String()

		taskID, err := cm.StartContainer(map[string]string{
			"BENCH_CONCURRENCY": strconv.FormatInt(int64(c), 10),
			"BENCH_URL":         u.String(),
			"BENCH_DURATION":    duration.String(),
			"BENCH_TIMEOUT":     timeout.String(),
			"BENCH_RUN_ID":      runID,
			"BENCH_RUNNER_ID":   runnerID,
		})

		if err != nil {
			writeErr(w, errors.Wrap(err, "error starting container"))
			return
		}

		j.Tasks = append(j.Tasks, bench.Task{
			ID:          runnerID,
			ContainerID: taskID,
			Concurrency: c,
		})
	}

	err = sm.SaveJob(j)
	if err != nil {
		writeErr(w, errors.Wrap(err, "error saving job"))
		return
	}

	json.NewEncoder(w).Encode(&j)
}

func readyToStart(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("runId")
	runnerID := r.URL.Query().Get("runnerId")

	log.Println("readyToStart", runID, runnerID)

	task, err := sm.GetTask(runID, runnerID)
	if err != nil {
		writeErr(w, errors.Wrap(err, "error getting task"))
		return
	}

	task.Ready = true

	err = sm.SaveTask(runID, task)
	if err != nil {
		writeErr(w, errors.Wrap(err, "error saving task"))
		return
	}
}

func waitForStart(w http.ResponseWriter, r *http.Request) {
	log.Println("waitForStart")

	runID := r.URL.Query().Get("runId")

	job, err := sm.GetJob(runID)
	if err != nil {
		writeErr(w, errors.Wrap(err, "error getting job"))
		return
	}

	ready := true
	for _, task := range job.Tasks {
		ready = ready && task.Ready
	}

	if ready {
		w.WriteHeader(200)
		return
	}

	w.WriteHeader(202)
}

func reportResult(w http.ResponseWriter, r *http.Request) {
	log.Println("reportResult")

	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}

	runID := r.URL.Query().Get("runId")
	runnerID := r.URL.Query().Get("runnerId")

	task, err := sm.GetTask(runID, runnerID)
	if err != nil {
		writeErr(w, errors.Wrap(err, "error getting task"))
		return
	}

	var result bench.Result
	err = json.NewDecoder(r.Body).Decode(&result)
	if err != nil {
		writeErr(w, errors.Wrap(err, "error decoding body"))
		return
	}

	task.Result = &result

	err = sm.SaveTask(runID, task)
	if err != nil {
		writeErr(w, errors.Wrap(err, "error saving task"))
		return
	}
}

func result(w http.ResponseWriter, r *http.Request) {
	log.Println("result")

	runID := r.URL.Query().Get("runId")

	job, err := sm.GetJob(runID)
	if err != nil {
		writeErr(w, errors.Wrap(err, "error getting job"))
		return
	}

	h := hdrhistogram.New(0, int64(hundredMicroSeconds(job.Timeout)), 2)

	result := bench.Result{
		StatusCodes: map[int]int{},
	}

	for _, task := range job.Tasks {
		if task.Result == nil {
			w.WriteHeader(403)
			w.Write([]byte("job not done"))
			return
		}

		current := task.Result

		currentH := hdrhistogram.Import(current.Histogram)
		h.Merge(currentH)

		result.Requests += current.Requests
		result.Timeouts += current.Timeouts
		result.Errors += current.Errors

		for k, v := range current.StatusCodes {
			result.StatusCodes[k] += v
		}
	}

	result.Histogram = h.Export()

	type summary struct {
		Max                   int64                  `json:"max"`
		Min                   int64                  `json:"min"`
		Mean                  float64                `json:"mean"`
		StdDev                float64                `json:"stddev"`
		TotalCount            int64                  `json:"totalCount"`
		HighestTrackableValue int64                  `json:"highestTrackableValue"`
		LowestTrackableValue  int64                  `json:"lowestTrackableValue"`
		Brackets              []hdrhistogram.Bracket `json:"brackets"`
	}

	s := summary{
		Max:                   h.Max(),
		Min:                   h.Min(),
		Mean:                  h.Mean(),
		StdDev:                h.StdDev(),
		TotalCount:            h.TotalCount(),
		HighestTrackableValue: h.HighestTrackableValue(),
		LowestTrackableValue:  h.LowestTrackableValue(),
		Brackets:              h.CumulativeDistribution(),
	}

	output := struct {
		Job     bench.Job    `json:"job"`
		Summary summary      `json:"summary"`
		Result  bench.Result `json:"result"`
	}{
		Job:     job,
		Summary: s,
		Result:  result,
	}

	json.NewEncoder(w).Encode(&output)
}

func logs(w http.ResponseWriter, r *http.Request) {
	runnerID := r.URL.Query().Get("runnerId")

	log.Println("logs", runnerID)

	logs, err := cm.GetLogs(runnerID)
	if err != nil {
		writeErr(w, err)
		return
	}

	w.Write([]byte(logs))
}

func tasks(w http.ResponseWriter, r *http.Request) {
	log.Println("tasks")

	runID := r.URL.Query().Get("runId")

	j, err := sm.GetJob(runID)
	if err != nil {
		writeErr(w, err)
		return
	}

	for i := range j.Tasks {
		logs, _ := cm.GetLogs(j.Tasks[i].ContainerID)
		j.Tasks[i].Logs = logs
	}

	json.NewEncoder(w).Encode(&j.Tasks)
}

func hundredMicroSeconds(d time.Duration) int {
	us := int(d / time.Microsecond)
	return int(math.Round(float64(us / 100)))
}
