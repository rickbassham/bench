package bench

import (
	"context"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/codahale/hdrhistogram"
)

type Replacer interface {
	Replace(s string) string
}

type noopReplacer struct{}

func (noopReplacer) Replace(s string) string {
	return s
}

type Runner struct {
	runID    string
	runnerID string

	concurrency int
	duration    time.Duration
	timeout     time.Duration
	url         string
	replacer    Replacer

	wg sync.WaitGroup

	startTime time.Time
	endTime   time.Time

	runOutput chan singleResult

	result Result
}

func (r *Result) Hist() *hdrhistogram.Histogram {
	if r.h != nil {
		return r.h
	}

	if r.Histogram != nil {
		r.h = hdrhistogram.Import(r.Histogram)
		return r.h
	}

	return nil
}

func NewRunner(concurrency int, duration, timeout time.Duration, url string, replacer Replacer) *Runner {
	if timeout == 0 || timeout > 2*time.Second {
		timeout = 2 * time.Second
	}

	if replacer == nil {
		replacer = noopReplacer{}
	}

	return &Runner{
		concurrency: concurrency,
		duration:    duration,
		timeout:     timeout,
		url:         url,
		replacer:    replacer,
		runOutput:   make(chan singleResult, 1000),
	}
}

func (r *Runner) Run() Result {
	r.startTime = time.Now()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		r.combineResults()
		wg.Done()
	}()

	r.wg.Add(r.concurrency)

	for i := 0; i < r.concurrency; i++ {
		go r.run(i)
	}

	// Wait for our concurrent runners to finish.
	r.wg.Wait()

	close(r.runOutput)

	r.endTime = time.Now()

	// Wait for combineResults to finish.
	wg.Wait()

	return r.result
}

func (r *Runner) run(index int) {
	runStart := time.Now()

	for time.Now().Sub(runStart) < r.duration {
		r.doRequest()
	}

	r.wg.Done()
}

type singleResult struct {
	StatusCode                  int
	DurationHundredMicroSeconds int
	Bytes                       int
	Timeout                     bool
	Err                         bool
}

func hundredMicroSeconds(d time.Duration) int {
	us := int(d / time.Microsecond)
	return int(math.Round(float64(us / 100)))
}

type Timeout interface {
	Timeout() bool
}

func (r *Runner) doRequest() {
	var result singleResult
	defer func() {
		r.runOutput <- result
	}()

	url := r.replacer.Replace(r.url)

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
}

func (r *Runner) combineResults() {
	result := Result{
		StatusCodes: map[int]int{},
		h:           hdrhistogram.New(0, int64(hundredMicroSeconds(r.timeout)), 2),
	}

	for item := range r.runOutput {
		result.Requests++
		result.h.RecordValue(int64(item.DurationHundredMicroSeconds))

		if item.Err {
			result.Errors++
		}

		if item.Timeout {
			result.Timeouts++
		}

		result.StatusCodes[item.StatusCode]++
	}

	result.Histogram = result.h.Export()

	result.StartTime = r.startTime
	result.EndTime = r.endTime
	result.Time = r.endTime.Sub(r.startTime)

	r.result = result
}
