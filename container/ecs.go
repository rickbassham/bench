package container

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/pkg/errors"
)

type ECSService interface {
	RunTask(input *ecs.RunTaskInput) (*ecs.RunTaskOutput, error)
}

type ECS struct {
	ecs            ECSService
	taskDefinition string
	cluster        string
	subnets        []*string
	securityGroups []*string
	publicIP       *string
}

func NewECS(ecs ECSService, taskDefinition, cluster string, subnets, securityGroups []string, publicIP bool) *ECS {
	var assignPublicIP string
	if publicIP {
		assignPublicIP = "ENABLED"
	} else {
		assignPublicIP = "DISABLED"
	}

	return &ECS{
		ecs:            ecs,
		taskDefinition: taskDefinition,
		cluster:        cluster,
		subnets:        aws.StringSlice(subnets),
		securityGroups: aws.StringSlice(securityGroups),
		publicIP:       aws.String(assignPublicIP),
	}
}

func (c *ECS) StartContainer(env map[string]string) (string, error) {
	envOverride := []*ecs.KeyValuePair{}

	for k, v := range env {
		envOverride = append(envOverride, &ecs.KeyValuePair{
			Name:  aws.String(k),
			Value: aws.String(v),
		})
	}

	output, err := c.ecs.RunTask(&ecs.RunTaskInput{
		Cluster:        aws.String(c.cluster),
		TaskDefinition: aws.String(c.taskDefinition),
		Overrides: &ecs.TaskOverride{
			ContainerOverrides: []*ecs.ContainerOverride{
				&ecs.ContainerOverride{
					Environment: envOverride,
				},
			},
		},
		NetworkConfiguration: &ecs.NetworkConfiguration{
			AwsvpcConfiguration: &ecs.AwsVpcConfiguration{
				AssignPublicIp: c.publicIP,
				SecurityGroups: c.securityGroups,
				Subnets:        c.subnets,
			},
		},
	})

	if err != nil {
		return "", errors.Wrap(err, "error running task")
	}

	return *output.Tasks[0].TaskArn, nil
}

func (c *ECS) GetLogs(id string) (string, error) {
	return "", nil
}
