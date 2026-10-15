package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

type fakeDocker struct {
	mu            sync.Mutex
	createCount   int
	removeCount   int
	createOptions client.ContainerCreateOptions
	removeOptions client.ContainerRemoveOptions
	startErr      error
	removeErr     error
	createEntered chan struct{}
	releaseCreate chan struct{}
}

func (f *fakeDocker) Events(context.Context, client.EventsListOptions) client.EventsResult {
	messages := make(chan events.Message)
	errs := make(chan error)
	close(messages)
	close(errs)
	return client.EventsResult{Messages: messages, Err: errs}
}

func (f *fakeDocker) ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{}, nil
}

func (f *fakeDocker) TaskList(context.Context, client.TaskListOptions) (client.TaskListResult, error) {
	return client.TaskListResult{}, nil
}

func (f *fakeDocker) ContainerCreate(_ context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	f.mu.Lock()
	f.createCount++
	f.createOptions = options
	f.mu.Unlock()
	if f.createEntered != nil {
		select {
		case f.createEntered <- struct{}{}:
		default:
		}
	}
	if f.releaseCreate != nil {
		<-f.releaseCreate
	}
	return client.ContainerCreateResult{ID: "temporary"}, nil
}

func (f *fakeDocker) ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error) {
	return client.ContainerStartResult{}, f.startErr
}

func (f *fakeDocker) ContainerWait(context.Context, string, client.ContainerWaitOptions) client.ContainerWaitResult {
	results := make(chan container.WaitResponse, 1)
	errs := make(chan error, 1)
	results <- container.WaitResponse{StatusCode: 0}
	return client.ContainerWaitResult{Result: results, Error: errs}
}

func (f *fakeDocker) ContainerRemove(_ context.Context, _ string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	f.mu.Lock()
	f.removeCount++
	f.removeOptions = options
	f.mu.Unlock()
	return client.ContainerRemoveResult{}, f.removeErr
}

func testConfig(t *testing.T) config {
	t.Helper()
	config, err := loadConfig(func(string) string { return "/mnt" })
	if err != nil {
		t.Fatal(err)
	}
	config.repairTimeout = time.Second
	config.cleanupTimeout = time.Second
	return config
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRepairAlwaysRemovesTemporaryContainer(t *testing.T) {
	docker := &fakeDocker{startErr: errors.New("start failed")}
	app := newApp(docker, testConfig(t), testLogger())

	if err := app.repair(context.Background(), "image", "/mnt/data"); err == nil {
		t.Fatal("expected start failure")
	}
	if docker.removeCount != 1 {
		t.Fatalf("remove count = %d, want 1", docker.removeCount)
	}
}

func TestRepairPreservesJavaContainerContract(t *testing.T) {
	docker := &fakeDocker{}
	app := newApp(docker, testConfig(t), testLogger())

	if err := app.repair(context.Background(), "failed-image", "/mnt/data"); err != nil {
		t.Fatal(err)
	}

	options := docker.createOptions
	if options.Config == nil || options.HostConfig == nil {
		t.Fatal("temporary container config is incomplete")
	}
	if options.Config.Image != "failed-image" {
		t.Fatalf("image = %q, want failed-image", options.Config.Image)
	}
	if len(options.Config.Entrypoint) != 1 || options.Config.Entrypoint[0] != "/bin/sh" {
		t.Fatalf("entrypoint = %v, want [/bin/sh]", options.Config.Entrypoint)
	}
	if len(options.Config.Cmd) != 2 || options.Config.Cmd[0] != "-c" || options.Config.Cmd[1] != "true" {
		t.Fatalf("command = %v, want [-c true]", options.Config.Cmd)
	}
	if options.Config.Labels[repairLabel] != "true" {
		t.Fatalf("repair label = %q, want true", options.Config.Labels[repairLabel])
	}
	if !options.HostConfig.Privileged {
		t.Fatal("temporary container must be privileged")
	}
	if !options.HostConfig.AutoRemove {
		t.Fatal("temporary container must enable Docker auto-removal")
	}
	if len(options.HostConfig.Binds) != 1 || !strings.HasPrefix(options.HostConfig.Binds[0], "/mnt/data:/volume-init-") {
		t.Fatalf("bind = %v, want legacy /mnt/data:/volume-init-* bind", options.HostConfig.Binds)
	}
	if !docker.removeOptions.Force {
		t.Fatal("explicit cleanup must force removal")
	}
}

func TestRepairToleratesFailedImageWithoutShell(t *testing.T) {
	docker := &fakeDocker{startErr: errors.New("exec: /bin/sh: no such file or directory")}
	app := newApp(docker, testConfig(t), testLogger())

	if err := app.repair(context.Background(), "shell-less-image", "/mnt/data"); err != nil {
		t.Fatal(err)
	}
	if docker.removeCount != 1 {
		t.Fatalf("remove count = %d, want 1", docker.removeCount)
	}
}

func TestRepairIgnoresCleanupConflict(t *testing.T) {
	docker := &fakeDocker{removeErr: errdefs.ErrConflict}
	app := newApp(docker, testConfig(t), testLogger())

	if err := app.repair(context.Background(), "image", "/mnt/data"); err != nil {
		t.Fatal(err)
	}
}

func TestProcessErrorContextDeduplicatesHostPath(t *testing.T) {
	docker := &fakeDocker{
		createEntered: make(chan struct{}, 1),
		releaseCreate: make(chan struct{}),
	}
	app := newApp(docker, testConfig(t), testLogger())
	ctx := errorContext{
		imageID: "image",
		error:   "failed to mount local volume: mount /mnt/data:/var/lib/docker/volumes/data/_data, no such file or directory",
		matches: func(mountFailure) bool { return true },
	}

	done := make(chan error, 1)
	go func() { done <- app.processErrorContext(context.Background(), ctx) }()
	<-docker.createEntered
	if err := app.processErrorContext(context.Background(), ctx); err != nil {
		t.Fatal(err)
	}
	close(docker.releaseCreate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if docker.createCount != 1 {
		t.Fatalf("create count = %d, want 1", docker.createCount)
	}
}
