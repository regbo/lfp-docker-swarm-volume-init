package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

type fakeDocker struct {
	mu            sync.Mutex
	createCount   int
	removeCount   int
	startErr      error
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

func (f *fakeDocker) ContainerCreate(context.Context, client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	f.mu.Lock()
	f.createCount++
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

func (f *fakeDocker) ContainerRemove(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	f.mu.Lock()
	f.removeCount++
	f.mu.Unlock()
	return client.ContainerRemoveResult{}, nil
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
