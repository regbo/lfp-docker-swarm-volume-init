package main

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/lainio/err2"
	"github.com/lainio/err2/assert"
	"github.com/lainio/err2/try"
	"github.com/sirupsen/logrus"
	"time"
)

//go:generate gonstructor --type=App --constructorTypes=builder -init construct -propagateInitFuncReturns
type App struct {
	config  *Config
	context context.Context
	client  *client.Client
	log     *logrus.Entry `gonstructor:"-"`
}

func (x *App) construct() error {
	if x.config == nil {
		x.config = GetConfig()
	}
	if x.context == nil {
		x.context = context.Background()
	}
	if x.client == nil {
		var err error
		if x.client, err = client.NewClientWithOpts(client.FromEnv); err != nil {
			return err
		}
	}
	logger := logrus.New()
	if level, err := logrus.ParseLevel(x.config.LogLevel); err != nil {
		return err
	} else {
		logger.SetLevel(level)
	}
	x.log = logger.WithField("containerID", x.config.ContainerID)
	return nil
}

func (x *App) Run() (_err error) {
	defer err2.Handle(&_err)
	x.log.Info("starting...")
	task := try.To1(x.task())
	stackName := task.Spec.ContainerSpec.Labels["com.docker.stack.namespace"]
	assert.NotEmpty(stackName, fmt.Sprintf("stackName not found. containerID:%v", x.config.ContainerID))
	x.log.WithField("stackName", stackName).Info("discovered stackName")
	var mountMappings []string
	for _, mount := range task.Spec.ContainerSpec.Mounts {
		if mountMapping := x.mountMapping(mount); mountMapping != "" {
			mountMappings = append(mountMappings, mountMapping)
		}
	}
	since := time.Now()
	for {
		volumeInit := NewVolumeInitBuilder().App(x).StackName(stackName).MountPaths(mountPaths).Since(since).Build()
		if modCount, err := volumeInit.Run(); err != nil {
			return err
		} else {
			x.log.WithField("modified", modCount).Debug("init complete")
		}
		since = time.Now()
		time.Sleep(x.config.PollInterval)
	}
}

func (x *App) task() (swarm.Task, error) {
	tasks, err := x.client.TaskList(x.context, types.TaskListOptions{})
	if err != nil {
		return swarm.Task{}, err
	}
	for _, task := range tasks {
		if task.Status.ContainerStatus == nil {
			continue
		}
		if x.config.ContainerID == task.Status.ContainerStatus.ContainerID {
			return task, nil
		}
	}
	return swarm.Task{}, fmt.Errorf("task not found. containerID:%v", x.config.ContainerID)
}

func (x *App) mountMapping(mount mount.Mount) *MountMapping {
	if mount.Target == "" {
		return nil
	}
	if mount.VolumeOptions == nil {
		return nil
	}
	if mount.VolumeOptions.DriverConfig == nil {
		return nil
	}
	driverConfig := mount.VolumeOptions.DriverConfig
	if "local" != driverConfig.Name {
		return nil
	}
	if device, ok := driverConfig.Options["device"]; !ok || device == "" {
		return nil
	} else {
		return &MountMapping{mount.Target, device}
	}
}
