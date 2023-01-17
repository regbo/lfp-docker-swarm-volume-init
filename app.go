package main

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/jellydator/ttlcache/v3"
	"github.com/lainio/err2"
	"github.com/lainio/err2/assert"
	"github.com/lainio/err2/try"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
	"io"
	"strings"
	"time"
)

const minInterval = time.Millisecond * 250

//go:generate gonstructor --type=App --constructorTypes=builder -init construct -propagateInitFuncReturns
type App struct {
	config          *Config
	context         context.Context
	client          *client.Client
	image           string
	devicePathCache *ttlcache.Cache[string, bool] `gonstructor:"-"`
	log             *logrus.Logger                `gonstructor:"-"`
}

func (x *App) construct() (_err error) {
	defer err2.Handle(&_err)
	if x.config == nil {
		x.config = GetConfig()
	}
	for key, interval := range map[string]time.Duration{"PollInterval": x.config.PollInterval, "MkdirInterval": x.config.MkdirInterval} {
		assert.That(interval >= minInterval, fmt.Sprintf("%v(%v) must be >=%v", key, interval, minInterval))
	}
	if x.context == nil {
		x.context = context.Background()
	}
	if x.client == nil {
		x.client = try.To1(client.NewClientWithOpts(client.FromEnv))
	}
	containerJSON := try.To1(x.client.ContainerInspect(x.context, x.config.ContainerID))
	x.image = containerJSON.Image
	x.devicePathCache = ttlcache.New[string, bool](ttlcache.WithDisableTouchOnHit[string, bool]())
	x.log = logrus.New()
	x.log.SetLevel(try.To1(logrus.ParseLevel(x.config.LogLevel)))
	return nil
}

func (x *App) Run() error {
	x.log.WithFields(x.config.LogFields()).Info("starting")
	for i := 0; ; i++ {
		logEntry := x.log.WithField("pass", i)
		logEntry.Info("poll started")
		devicePaths, err := x.poll()
		if len(devicePaths) > 0 {
			logEntry = logEntry.WithField("devicePaths", devicePaths)
		}
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("poll error:%v", logEntry.Data))
		}
		logEntry.Info("poll complete")
		time.Sleep(x.config.PollInterval)
	}
}

func (x *App) poll() (_ []string, _err error) {
	defer err2.Handle(&_err)
	volumes := try.To1(x.client.VolumeList(x.context, filters.Args{}))
	var devicePaths []string
	if volumes.Volumes != nil {
		for _, volume := range volumes.Volumes {
			if "local" != volume.Driver {
				continue
			}
			devicePath, ok := volume.Options["device"]
			if !ok || devicePath == "" || slices.Contains(devicePaths, devicePath) {
				continue
			}
			devicePaths = append(devicePaths, devicePath)
		}
	}
	devicePaths = try.To1(x.filterDevicePaths(devicePaths))
	try.To(x.mkDevicePaths(devicePaths))
	return devicePaths, nil
}

func (x *App) filterDevicePaths(devicePaths []string) (_ []string, _err error) {
	defer err2.Handle(&_err)
	for i := 0; i < len(devicePaths); i++ {
		devicePath := devicePaths[i]
		item := x.devicePathCache.Get(devicePath)
		if item != nil {
			devicePaths = slices.Delete(devicePaths, i, i+1)
			i--
		}
	}
	if len(devicePaths) == 0 {
		return nil, nil
	}
	slices.Sort(devicePaths)
	var printCommand string
	for _, devicePath := range devicePaths {
		printCommand += fmt.Sprintf("DIR='%v' && [ -d \"${DIR}\" ] && echo -n \"${DIR}|\";", devicePath)
	}
	command := []string{"-t", "1", "-m", "-u", "-n", "-i", "sh", "-c", printCommand}
	buf := try.To1(x.containerRun("nsenter", command, map[string]string{"/var/run/docker.sock": "/var/run/docker.sock"}, true))
	for _, devicePath := range strings.Split(string(buf), "|") {
		index := slices.Index(devicePaths, devicePath)
		if index >= 0 {
			devicePaths = slices.Delete(devicePaths, index, index+1)
			x.devicePathCache.Set(devicePath, false, x.config.MkdirInterval)
		}
	}
	return devicePaths, nil
}

func (x *App) mkDevicePaths(devicePaths []string) (_err error) {
	defer err2.Handle(&_err)
	if len(devicePaths) == 0 {
		return nil
	}
	bindMounts := make(map[string]string)
	for i, devicePath := range devicePaths {
		bindMounts[devicePath] = fmt.Sprintf("/mnt/v_%v", i)
	}
	_, err := x.containerRun("/bin/sh", []string{"-c", "exit 0"}, bindMounts, false)
	return err
}

func (x *App) containerRun(entrypoint string, command []string, bindMounts map[string]string, pidModeHost bool) (_ []byte, _err error) {
	defer err2.Handle(&_err)
	var binds []string
	for host, container := range bindMounts {
		binds = append(binds, fmt.Sprintf("%v:%v", host, container))
	}
	hostConfig := &container.HostConfig{
		Privileged: true,
		Binds:      binds,
	}
	if pidModeHost {
		hostConfig.PidMode = "host"
	}
	createdContainer := try.To1(x.client.ContainerCreate(x.context, &container.Config{
		Image:      x.image,
		Entrypoint: []string{entrypoint},
		Cmd:        command,
	}, hostConfig, nil, nil, ""))
	logEntry := x.log.WithField("containerID", createdContainer.ID)
	logEntry.Debug("container created")
	defer func() {
		if err := x.client.ContainerRemove(x.context, createdContainer.ID, types.ContainerRemoveOptions{}); err != nil {
			panic(err)
		}
	}()
	try.To(x.client.ContainerStart(x.context, createdContainer.ID, types.ContainerStartOptions{}))
	conditionChan, errorChan := x.client.ContainerWait(x.context, createdContainer.ID, container.WaitConditionNotRunning)
	var ok bool
	for !ok {
		select {
		case <-conditionChan:
			ok = true
		case err := <-errorChan:
			return nil, err
		}
	}
	if err := x.containerError(createdContainer.ID); err != nil {
		return nil, err
	}
	return x.containerLogs(createdContainer.ID)
}

func (x *App) containerLogs(containerID string) (_ []byte, _err error) {
	defer err2.Handle(&_err)
	reader := try.To1(x.client.ContainerLogs(x.context, containerID, types.ContainerLogsOptions{
		Timestamps: false,
		Details:    false,
		ShowStdout: true,
		ShowStderr: true,
	}))
	defer func() {
		_ = reader.Close()
	}()
	if _, err := io.ReadFull(reader, make([]byte, 8)); err != nil {
		if io.EOF == err || io.ErrUnexpectedEOF == err {
			return nil, nil
		}
		return nil, err
	}
	if buf, err := io.ReadAll(reader); err != nil {
		return nil, err
	} else {
		return buf, nil
	}
}

func (x *App) containerError(containerID string) (_err error) {
	defer err2.Handle(&_err)
	containerInspect := try.To1(x.client.ContainerInspect(x.context, containerID))
	if containerInspect.State == nil {
		return errors.New(fmt.Sprintf("state not found"))
	}
	if containerInspect.State.ExitCode == 0 {
		return nil
	}
	if containerInspect.State.Error != "" {
		return errors.New(containerInspect.State.Error)
	}
	out := string(try.To1(x.containerLogs(containerID)))
	out = strings.TrimSuffix(out, "\n")
	if out != "" {
		return errors.New(out)
	}
	return errors.New(fmt.Sprintf("exit code:%v", containerInspect.State.ExitCode))
}
