package main

import (
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/lainio/err2"
	"github.com/lainio/err2/try"
	errors2 "github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:generate gonstructor --type=VolumeInit --constructorTypes=builder -init construct -propagateInitFuncReturns
type VolumeInit struct {
	app        *App
	stackName  string
	mountPaths []string
	since      time.Time
	logFields  log.Fields `gonstructor:"-"`
}

func (x *VolumeInit) construct() {
	x.logFields = log.Fields{
		"stackName":  x.stackName,
		"mountPaths": x.mountPaths,
	}
}

func (x *VolumeInit) Run() (_ int, _err error) {
	defer err2.Handle(&_err)
	tasks := try.To1(x.app.client.TaskList(x.app.context, types.TaskListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", fmt.Sprintf("com.docker.stack.namespace=%v", x.stackName)),
		),
	}))
	var modCount int
	for _, task := range tasks {
		if task.Meta.CreatedAt.Before(x.since) {
			continue
		}
		if swarm.TaskStateRunning == task.Status.State {
			continue
		}
		if !strings.HasSuffix(task.Status.Err, "no such file or directory") {
			continue
		}
		for _, mount := range task.Spec.ContainerSpec.Mounts {
			path := x.app.localMountPath(mount)
			if path == "" {
				continue
			}
			if mod, err := x.init(path); err != nil {
				return modCount, errors2.Wrap(err, fmt.Sprintf("%v", x.logFields))
			} else if mod {
				modCount++
			}
		}
	}
	return modCount, nil
}

func (x *VolumeInit) init(path string) (bool, error) {
	if err := x.validatePath(path); err != nil {
		return false, err
	}
	if _, err := os.Stat(path); err == nil {
		log.WithFields(x.logFields).WithField("path", path).Debug("skipping")
		return false, nil
	}
	if err := os.MkdirAll(path, os.ModePerm); err != nil {
		return false, errors2.Wrap(err, fmt.Sprintf("mkdir failed. path:%v", path))
	}
	log.WithFields(x.logFields).WithField("path", path).Info("created")
	return true, nil
}

func (x *VolumeInit) validatePath(path string) error {
	for _, mountPath := range x.mountPaths {
		if mountPath == path {
			return nil
		}
		if matched, err := filepath.Match(fmt.Sprintf("%v%v*", mountPath, filepath.Separator), path); err == nil && matched {
			return nil
		}
	}
	return fmt.Errorf("not mounted. path:%v", path)
}
