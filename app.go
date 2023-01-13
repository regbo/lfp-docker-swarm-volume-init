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
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"os"
	"time"
)

//go:generate gonstructor --type=App --constructorTypes=builder -init construct -propagateInitFuncReturns
type App struct {
	context     context.Context
	client      *client.Client
	containerID string
}

func (x *App) construct() error {
	if x.context == nil {
		x.context = context.Background()
	}
	if x.client == nil {
		var err error
		if x.client, err = client.NewClientWithOpts(client.FromEnv); err != nil {
			return err
		}
	}
	if x.containerID == "" {
		x.containerID = os.Getenv("CONTAINER_ID")
		if x.containerID == "" {
			return errors.New("containerID not found")
		}
	}
	return nil
}

func (x *App) Run() (_err error) {
	defer err2.Handle(&_err)
	task := try.To1(x.task())
	stackName := task.Spec.ContainerSpec.Labels["com.docker.stack.namespace"]
	assert.NotEmpty(stackName, fmt.Sprintf("stackName not found. containerID:%v", x.containerID))
	var mountPaths []string
	for _, mount := range task.Spec.ContainerSpec.Mounts {
		if mountPath := x.localMountPath(mount); mountPath != "" {
			mountPaths = append(mountPaths, mountPath)
		}
	}
	since := time.Now()
	for {
		volumeInit := NewVolumeInitBuilder().App(x).StackName(stackName).MountPaths(mountPaths).Since(since).Build()
		if modCount, err := volumeInit.Run(); err != nil {
			return err
		} else {
			log.WithField("modified", modCount).Debug("init complete")
		}
		since = time.Now()
		time.Sleep(time.Second * 5)
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
		if x.containerID == task.Status.ContainerStatus.ContainerID {
			return task, nil
		}
	}
	return swarm.Task{}, fmt.Errorf("task not found. containerID:%v", x.containerID)
}

func (x *App) localMountPath(mount mount.Mount) string {
	if mount.VolumeOptions == nil {
		return ""
	}
	if mount.VolumeOptions.DriverConfig == nil {
		return ""
	}
	driverConfig := mount.VolumeOptions.DriverConfig
	if "local" != driverConfig.Name {
		return ""
	}
	if device, ok := driverConfig.Options["device"]; !ok {
		return ""
	} else {
		return device
	}
}
