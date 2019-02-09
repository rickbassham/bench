package bench

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/codahale/hdrhistogram"
)

type Runner struct {
	runID    string
	runnerID string

	concurrency int
	duration    time.Duration
	timeout     time.Duration
	url         string

	wg sync.WaitGroup

	results []Result
}

type Result struct {
	h *hdrhistogram.Histogram

	Requests    int                    `json:"requests"`
	Errors      int                    `json:"errors"`
	Timeouts    int                    `json:"timeouts"`
	StatusCodes map[int]int            `json:"statusCodes"`
	Time        time.Duration          `json:"time"`
	Histogram   *hdrhistogram.Snapshot `json:"histogram"`
}

func NewRunner(concurrency int, duration, timeout time.Duration, url string) *Runner {
	if timeout == 0 || timeout > 2*time.Second {
		timeout = 2 * time.Second
	}

	results := make([]Result, concurrency)
	for i := 0; i < concurrency; i++ {
		results[i].h = hdrhistogram.New(0, int64(hundredMicroSeconds(timeout)), 2)
		results[i].StatusCodes = map[int]int{}
	}

	return &Runner{
		concurrency: concurrency,
		duration:    duration,
		timeout:     timeout,
		url:         url,
		results:     results,
	}
}

func (r *Runner) Start() {
	r.wg.Add(r.concurrency)

	for i := 0; i < r.concurrency; i++ {
		go r.run(i)
	}
}

func (r *Runner) Wait() {
	r.wg.Wait()
}

func (r *Runner) run(index int) {
	runStart := time.Now()

	for time.Now().Sub(runStart) < r.duration {
		result := r.doRequest()

		r.results[index].h.RecordValue(int64(result.DurationHundredMicroSeconds))
		r.results[index].Requests++

		if result.Err {
			r.results[index].Errors++
		}

		if result.Timeout {
			r.results[index].Timeouts++
		}

		r.results[index].StatusCodes[result.StatusCode]++
	}

	r.results[index].Time = time.Now().Sub(runStart)

	r.wg.Done()
}

type SingleResult struct {
	StatusCode                  int  `json:"s"`
	DurationHundredMicroSeconds int  `json:"d"`
	Bytes                       int  `json:"b"`
	Timeout                     bool `json:"t"`
	Err                         bool `json:"e"`
}

func hundredMicroSeconds(d time.Duration) int {
	us := int(d / time.Microsecond)
	return int(math.Round(float64(us/100))*100) / 100
}

type Timeout interface {
	Timeout() bool
}

func (r *Runner) doRequest() (result SingleResult) {
	randomKey, _ := rand.Int(rand.Reader, big.NewInt(50000000))

	url := fmt.Sprintf("%s%d", r.url, randomKey.Int64())

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		result.Err = true
		return
	}

	ctx, cancel := context.WithTimeout(req.Context(), r.timeout)
	defer cancel()
	req = req.WithContext(ctx)

	start := time.Now()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		result.Err = true
		if t, ok := err.(Timeout); ok {
			result.Timeout = t.Timeout()
		}
	}

	if resp != nil {
		result.StatusCode = resp.StatusCode

		if resp.Body != nil {
			n, _ := io.Copy(ioutil.Discard, resp.Body)
			result.Bytes = int(n)
			resp.Body.Close()
		}
	}

	result.DurationHundredMicroSeconds = hundredMicroSeconds(time.Now().Sub(start))

	return
}

func (r *Runner) Result() Result {
	result := Result{
		StatusCodes: map[int]int{},
	}

	result.Time = r.duration

	for i := 0; i < r.concurrency; i++ {
		current := r.results[i]

		if i == 0 {
			result.h = current.h
		} else {
			result.h.Merge(current.h)
		}

		result.Requests += current.Requests
		result.Timeouts += current.Timeouts
		result.Errors += current.Errors

		for k, v := range current.StatusCodes {
			result.StatusCodes[k] += v
		}
	}

	result.Histogram = result.h.Export()

	return result
}
