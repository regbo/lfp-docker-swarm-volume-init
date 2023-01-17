package main

import (
	"context"
	"fmt"
	"github.com/docker/distribution/uuid"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/lainio/err2"
	"github.com/lainio/err2/try"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
	"os/exec"
	"time"
)

//go:generate gonstructor --type=App --constructorTypes=builder -init construct -propagateInitFuncReturns
type App struct {
	config  *Config
	context context.Context
	client  *client.Client
	log     *logrus.Logger `gonstructor:"-"`
}

func (x *App) construct() (_err error) {
	defer err2.Handle(&_err)
	if x.config == nil {
		x.config = GetConfig()
	}
	minPollInterval := time.Millisecond * 250
	if x.config.PollInterval < minPollInterval {
		return fmt.Errorf("poll interval must be >=%v", minPollInterval)
	}
	if x.context == nil {
		x.context = context.Background()
	}
	if x.client == nil {
		x.client = try.To1(client.NewClientWithOpts(client.FromEnv))
	}
	x.log = logrus.New()
	x.log.SetLevel(try.To1(logrus.ParseLevel(x.config.LogLevel)))
	return nil
}

func (x *App) Run() error {
	logFields := logrus.Fields{}
	if x.log.IsLevelEnabled(logrus.DebugLevel) {
		for k, v := range x.config.Map() {
			logFields[k] = v
		}
	}
	x.log.WithFields(logFields).Info("starting")
	var lastPoll = time.Now().Add(-1 * x.config.PollSince)
	for i := 0; ; i++ {
		logEntry := x.log.WithField("pass", i)
		logEntry.Info("poll started")
		since := lastPoll
		lastPoll = time.Now()
		modCount, err := x.poll(since)
		logEntry = logEntry.WithField("modCount", modCount)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("poll error:%v", logEntry.Data))
		}
		logEntry.Info("poll complete")
		time.Sleep(x.config.PollInterval)
	}
}

func (x *App) poll(since time.Time) (_ int, _err error) {
	defer err2.Handle(&_err)
	devicePaths := try.To1(x.devicePaths(since))
	devicePathCount := len(devicePaths)
	if devicePathCount == 0 {
		return 0, nil
	}
	dockerRunCommand := "docker run -t --rm --privileged"
	for _, devicePath := range devicePaths {
		dockerRunCommand += fmt.Sprintf(" -v %v:/mnt/volume_%v", devicePath, uuid.Generate())
	}
	dockerRunCommand += " alpine:latest sh -c \"echo container started\""
	runLog := x.log.WithField("devicePaths", devicePaths)
	if x.log.IsLevelEnabled(logrus.DebugLevel) {
		runLog = runLog.WithField("dockerRunCommand", dockerRunCommand)
	}
	runLog.Info("docker run started")
	cmd := exec.Command("sh", "-c", dockerRunCommand)
	stdout, err := cmd.Output()
	outputLog := x.log.WithField("stdout", string(stdout))
	if err != nil {
		outputLog = outputLog.WithField("err", err)
	}
	outputLog.Debug("docker run complete")
	if err != nil {
		return 0, err
	}
	return devicePathCount, nil
}
func (x *App) devicePaths(since time.Time) (_ []string, _err error) {
	defer err2.Handle(&_err)
	var devicePaths []string
	containers := try.To1(x.client.ContainerList(x.context, types.ContainerListOptions{
		All: true,
	}))
	for _, container := range containers {
		for _, devicePath := range x.containerDevicePaths(since, container) {
			if !slices.Contains(devicePaths, devicePath) {
				devicePaths = append(devicePaths, devicePath)
			}
		}
	}
	//error if not swarm mode
	tasks, _ := x.client.TaskList(x.context, types.TaskListOptions{})
	var nodeID string
	for _, task := range tasks {
		if task.Status.ContainerStatus != nil && task.Status.ContainerStatus.ContainerID == x.config.ContainerID {
			nodeID = task.NodeID
			break
		}
	}
	if nodeID != "" {
		for _, task := range tasks {
			for _, devicePath := range x.taskDevicePaths(since, nodeID, task) {
				if !slices.Contains(devicePaths, devicePath) {
					devicePaths = append(devicePaths, devicePath)
				}
			}
		}
	}
	slices.Sort(devicePaths)
	return devicePaths, nil
}

func (x *App) containerDevicePaths(since time.Time, container types.Container) []string {
	log := x.log.WithField("containerID", container.ID)
	if x.config.ContainerID == container.ID {
		log.Trace("self lookup, skipping")
		return nil
	}
	if "running" == container.State {
		log.Trace("running state, skipping")
		return nil
	}
	createdAt := time.Unix(container.Created, 0)
	if createdAt.Before(since) {
		log.Trace("stale container, skipping")
		return nil
	}
	containerJSON, err := x.client.ContainerInspect(x.context, container.ID)
	if err != nil {
		log.WithError(err).Debug("inspect failed, skipping")
		return nil
	}
	if containerJSON.HostConfig == nil {
		log.Debug("HostConfig not found, skipping")
		return nil
	}
	return x.mountDevicePaths(containerJSON.HostConfig.Mounts)
}

func (x *App) taskDevicePaths(since time.Time, nodeID string, task swarm.Task) []string {
	log := x.log.WithField("nodeID", nodeID).WithField("taskID", task.ID)
	if nodeID != task.NodeID {
		log.Trace("nodeID differs, skipping")
		return nil
	}
	if task.Status.ContainerStatus != nil && task.Status.ContainerStatus.ContainerID == x.config.ContainerID {
		log.Trace("self lookup, skipping")
		return nil
	}
	if task.Status.State != swarm.TaskStateRejected && task.Status.State != swarm.TaskStateFailed {
		log.Trace("normal state, skipping")
		return nil
	}
	if task.CreatedAt.Before(since) {
		log.Trace("stale task, skipping")
		return nil
	}
	if task.Spec.ContainerSpec == nil {
		log.Debug("ContainerSpec not found, skipping")
		return nil
	}
	return x.mountDevicePaths(task.Spec.ContainerSpec.Mounts)
}

func (x *App) mountDevicePaths(mounts []mount.Mount) []string {
	var devicePaths []string
	for _, mount := range mounts {
		if mount.VolumeOptions == nil || mount.VolumeOptions.DriverConfig == nil || "local" != mount.VolumeOptions.DriverConfig.Name {
			continue
		}
		devicePath, ok := mount.VolumeOptions.DriverConfig.Options["device"]
		if !ok {
			continue
		}
		devicePaths = append(devicePaths, devicePath)
	}
	return devicePaths
}
