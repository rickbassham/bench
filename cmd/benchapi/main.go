package main

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/codahale/hdrhistogram"
	"github.com/go-redis/redis"
	"github.com/google/uuid"
	"github.com/spf13/viper"

	"github.com/rickbassham/bench"
	"github.com/rickbassham/bench/container"
	"github.com/rickbassham/bench/storage"
)

type ContainerManager interface {
	StartContainer(env map[string]string) (string, error)
}

type StorageManager interface {
	SaveTask(runID string, t storage.Task) error
	GetTask(runID, taskID string) (storage.Task, error)
	SaveJob(j storage.Job) error
	GetJob(runID string) (storage.Job, error)
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

	sess, err := session.NewSession()
	if err != nil {
		log.Println(err.Error())
		return
	}

	viper.SetEnvPrefix("bench")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	maxPerContainer = viper.GetInt("max-per-container")

	r := redis.NewClient(&redis.Options{
		Addr:     viper.GetString("redis-address"),
		Password: viper.GetString("redis-auth"),
	})

	cm = container.NewECS(ecs.New(sess), "", "")
	sm = storage.NewRedis(r)

	http.HandleFunc("/health", health)
	http.HandleFunc("/start", start)
	http.HandleFunc("/readyToStart", readyToStart)
	http.HandleFunc("/waitForStart", waitForStart)
	http.HandleFunc("/reportResult", reportResult)
	http.HandleFunc("/result", result)

	err = http.ListenAndServe(":3000", nil)
	if err != nil {
		log.Println(err.Error())
		return
	}
}

func health(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

func start(w http.ResponseWriter, r *http.Request) {
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

	for i := concurrency; i > 0; i -= maxPerContainer {
		c := maxPerContainer
		if i < c {
			c = i
		}

		cm.StartContainer(map[string]string{
			"BENCH_CONCURRENCY": strconv.FormatInt(int64(c), 10),
			"BENCH_URL":         u.String(),
			"BENCH_DURATION":    duration.String(),
			"BENCH_TIMEOUT":     timeout.String(),
			"BENCH_RUN_ID":      runID,
		})
	}

	w.Write([]byte(runID))
}

func readyToStart(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("runId")
	runnerID := r.URL.Query().Get("runnerId")

	task, err := sm.GetTask(runID, runnerID)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}

	task.Ready = true

	err = sm.SaveTask(runID, task)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}
}

func waitForStart(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("runId")

	job, err := sm.GetJob(runID)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
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
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}

	runID := r.URL.Query().Get("runId")
	runnerID := r.URL.Query().Get("runnerId")

	task, err := sm.GetTask(runID, runnerID)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}

	var result bench.Result
	err = json.NewDecoder(r.Body).Decode(&result)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}

	task.Result = &result

	err = sm.SaveTask(runID, task)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
		return
	}
}

func result(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("runId")

	job, err := sm.GetJob(runID)
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(err.Error()))
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

	json.NewEncoder(w).Encode(&result)
}

func hundredMicroSeconds(d time.Duration) int {
	us := int(d / time.Microsecond)
	return int(math.Round(float64(us / 100)))
}
