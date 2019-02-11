package bench

import (
	"time"

	"github.com/codahale/hdrhistogram"
)

type Task struct {
	ID          string  `json:"id"`
	ContainerID string  `json:"containerId"`
	Ready       bool    `json:"ready"`
	Result      *Result `json:"result"`
	Logs        string  `json:"logs"`
	Concurrency int     `json:"concurrency"`
}

type Job struct {
	RunID       string        `json:"runId"`
	Concurrency int           `json:"concurrency"`
	Duration    time.Duration `json:"duration"`
	Timeout     time.Duration `json:"timeout"`
	URL         string        `json:"url"`

	MetaData map[string]string `json:"meta"`

	RequestTime time.Time `json:"requestTime"`
	StartTime   time.Time `json:"startTime"`
	EndTime     time.Time `json:"endTime"`

	Tasks []Task `json:"tasks"`
}

type Result struct {
	h *hdrhistogram.Histogram

	Requests    int                    `json:"requests"`
	Errors      int                    `json:"errors"`
	Timeouts    int                    `json:"timeouts"`
	StatusCodes map[int]int            `json:"statusCodes"`
	Time        time.Duration          `json:"time"`
	Histogram   *hdrhistogram.Snapshot `json:"histogram"`
	StartTime   time.Time              `json:"startTime"`
	EndTime     time.Time              `json:"endTime"`
}
