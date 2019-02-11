package container

import (
	"context"
	"fmt"
	"io/ioutil"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
)

type Docker struct {
	c     *client.Client
	image string
}

func NewDocker(c *client.Client, image string) *Docker {
	return &Docker{
		c:     c,
		image: image,
	}
}

func (d *Docker) StartContainer(env map[string]string) (string, error) {
	var dockerEnv []string

	for k, v := range env {
		dockerEnv = append(dockerEnv, fmt.Sprintf(`%s=%s`, k, v))
	}

	output, err := d.c.ContainerCreate(context.Background(), &container.Config{
		Image: d.image,
		Env:   dockerEnv,
	}, nil, nil, "")
	if err != nil {
		return "", errors.Wrap(err, "error creating container")
	}

	err = d.c.ContainerStart(context.Background(), output.ID, types.ContainerStartOptions{})
	if err != nil {
		return "", errors.Wrap(err, "error starting container")
	}

	return output.ID, nil
}

func (d *Docker) GetLogs(id string) (string, error) {
	rdr, err := d.c.ContainerLogs(context.Background(), id, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return "", errors.Wrap(err, "error getting logs")
	}

	defer rdr.Close()

	log, err := ioutil.ReadAll(rdr)
	if err != nil {
		return "", errors.Wrap(err, "error reading logs")
	}

	return string(log), nil
}
