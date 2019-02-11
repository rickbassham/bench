package container

import (
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/pkg/errors"
)

type ECS interface {
	RunTask(input *ecs.RunTaskInput) (*ecs.RunTaskOutput, error)
}

type Logs interface {
	GetLogEvents(input *cloudwatchlogs.GetLogEventsInput) (*cloudwatchlogs.GetLogEventsOutput, error)
}

type AWS struct {
	ecs            ECS
	logs           Logs
	taskDefinition string
	logGroup       string
	cluster        string
	subnets        []*string
	securityGroups []*string
	publicIP       *string
}

func NewAWS(ecs ECS, logs Logs, taskDefinition, logGroup, cluster string, subnets, securityGroups []string, publicIP bool) *AWS {
	var assignPublicIP string
	if publicIP {
		assignPublicIP = "ENABLED"
	} else {
		assignPublicIP = "DISABLED"
	}

	return &AWS{
		ecs:            ecs,
		logs:           logs,
		taskDefinition: taskDefinition,
		logGroup:       logGroup,
		cluster:        cluster,
		subnets:        aws.StringSlice(subnets),
		securityGroups: aws.StringSlice(securityGroups),
		publicIP:       aws.String(assignPublicIP),
	}
}

func (c *AWS) StartContainer(env map[string]string) (string, error) {
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

func (c *AWS) GetLogs(id string) (string, error) {
	var logs []string

	var nextToken *string

	for {
		output, err := c.logs.GetLogEvents(&cloudwatchlogs.GetLogEventsInput{
			NextToken:     nextToken,
			StartFromHead: aws.Bool(true),
			LogGroupName:  aws.String(c.logGroup),
			LogStreamName: aws.String(id),
		})
		if err != nil {
			return "", errors.Wrap(err, "error getting log events")
		}

		nextToken = output.NextForwardToken

		for _, e := range output.Events {
			logs = append(logs, *e.Message)
		}

		if nextToken == nil {
			break
		}
	}

	return strings.Join(logs, "\n"), nil
}
