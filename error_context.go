package main

import (
	"path"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/swarm"
)

const mountErrorPrefix = "failed to mount local volume: mount "

type mountFailure struct {
	hostPath        string
	destinationPath string
}

func parseMountFailure(message string) (mountFailure, bool) {
	lower := strings.ToLower(message)
	if !strings.Contains(lower, "no such file or directory") {
		return mountFailure{}, false
	}

	start := strings.Index(lower, mountErrorPrefix)
	if start < 0 {
		return mountFailure{}, false
	}
	mount := message[start+len(mountErrorPrefix):]
	if before, _, found := strings.Cut(mount, ","); found {
		mount = before
	}
	hostPath, destinationPath, found := strings.Cut(mount, ":")
	if !found {
		return mountFailure{}, false
	}

	hostPath = strings.TrimSpace(hostPath)
	destinationPath = strings.TrimSpace(destinationPath)
	if !path.IsAbs(hostPath) || !path.IsAbs(destinationPath) {
		return mountFailure{}, false
	}
	return mountFailure{
		hostPath:        path.Clean(hostPath),
		destinationPath: path.Clean(destinationPath),
	}, true
}

type errorContext struct {
	imageID string
	error   string
	matches func(mountFailure) bool
}

func errorContextFromContainer(inspect container.InspectResponse) (errorContext, bool) {
	if inspect.Image == "" || inspect.State == nil || strings.TrimSpace(inspect.State.Error) == "" {
		return errorContext{}, false
	}

	paths := make(map[string]struct{})
	for _, mounted := range inspect.Mounts {
		if mounted.Driver != "local" {
			continue
		}
		if mounted.Source != "" {
			paths[path.Clean(mounted.Source)] = struct{}{}
		}
		if mounted.Destination != "" {
			paths[path.Clean(mounted.Destination)] = struct{}{}
		}
	}
	if len(paths) == 0 {
		return errorContext{}, false
	}

	return errorContext{
		imageID: inspect.Image,
		error:   inspect.State.Error,
		matches: func(failure mountFailure) bool {
			_, ok := paths[failure.destinationPath]
			return ok
		},
	}, true
}

func errorContextFromTask(task swarm.Task) (errorContext, bool) {
	spec := task.Spec.ContainerSpec
	if spec == nil || spec.Image == "" || strings.TrimSpace(task.Status.Err) == "" {
		return errorContext{}, false
	}

	devices := make(map[string]struct{})
	for _, mounted := range spec.Mounts {
		if mounted.VolumeOptions == nil || mounted.VolumeOptions.DriverConfig == nil {
			continue
		}
		driver := mounted.VolumeOptions.DriverConfig
		if driver.Name != "local" {
			continue
		}
		if device := strings.TrimSpace(driver.Options["device"]); path.IsAbs(device) {
			devices[path.Clean(device)] = struct{}{}
		}
	}
	if len(devices) == 0 {
		return errorContext{}, false
	}

	return errorContext{
		imageID: spec.Image,
		error:   task.Status.Err,
		matches: func(failure mountFailure) bool {
			_, ok := devices[failure.hostPath]
			return ok
		},
	}, true
}
