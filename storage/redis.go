package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"

	"github.com/rickbassham/bench"
)

type Client interface {
	Set(key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	SAdd(key string, members ...interface{}) *redis.IntCmd
	Get(key string) *redis.StringCmd
	SMembers(key string) *redis.StringSliceCmd
}

type Task struct {
	ID          string        `json:"id"`
	ContainerID string        `json:"containerId"`
	Ready       bool          `json:"ready"`
	Result      *bench.Result `json:"result"`
	Logs        string        `json:"logs"`
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

	Tasks []Task `json:"-"`
}

type Redis struct {
	r Client
}

func NewRedis(r Client) *Redis {
	return &Redis{
		r: r,
	}
}

func (r *Redis) SaveJob(j Job) error {
	jobData, err := json.Marshal(&j)
	if err != nil {
		return errors.Wrap(err, "error marshalling job")
	}

	_, err = r.r.Set(fmt.Sprintf("JOB_%s", j.RunID), string(jobData), 0).Result()
	if err != nil {
		return errors.Wrap(err, "error saving job data")
	}

	for _, t := range j.Tasks {
		err = r.SaveTask(j.RunID, t)
		if err != nil {
			return errors.Wrap(err, "error saving task")
		}

		_, err = r.r.SAdd(fmt.Sprintf("JOB_%s_TASKS", j.RunID), t.ID).Result()
		if err != nil {
			return errors.Wrap(err, "error saving task id")
		}
	}

	return nil
}

func (r *Redis) GetJob(runID string) (Job, error) {
	var j Job

	log.Println(fmt.Sprintf("JOB_%s", runID))

	jobData, err := r.r.Get(fmt.Sprintf("JOB_%s", runID)).Result()
	if err != nil {
		return j, errors.Wrap(err, "error getting job data")
	}

	err = json.Unmarshal([]byte(jobData), &j)
	if err != nil {
		return j, errors.Wrap(err, "error unmarshalling job")
	}

	taskIDs, err := r.r.SMembers(fmt.Sprintf("JOB_%s_TASKS", runID)).Result()
	if err != nil {
		return j, errors.Wrap(err, "error getting tasks data")
	}

	for _, taskID := range taskIDs {
		t, err := r.GetTask(runID, taskID)
		if err != nil {
			return j, errors.Wrap(err, "error getting task")
		}

		j.Tasks = append(j.Tasks, t)
	}

	return j, nil
}

func (r *Redis) GetTask(runID, taskID string) (Task, error) {
	var t Task

	log.Println("GET", fmt.Sprintf("JOB_%s_TASK_%s", runID, taskID))

	taskData, err := r.r.Get(fmt.Sprintf("JOB_%s_TASK_%s", runID, taskID)).Result()
	if err != nil {
		return t, errors.Wrap(err, "error getting task data")
	}

	err = json.Unmarshal([]byte(taskData), &t)
	if err != nil {
		return t, errors.Wrap(err, "error unmarshalling task")
	}

	return t, nil
}

func (r *Redis) SaveTask(runID string, t Task) error {
	taskData, err := json.Marshal(&t)
	if err != nil {
		return errors.Wrap(err, "error marshalling task")
	}

	_, err = r.r.Set(fmt.Sprintf("JOB_%s_TASK_%s", runID, t.ID), string(taskData), 0).Result()
	if err != nil {
		return errors.Wrap(err, "error saving task data")
	}

	return nil
}
