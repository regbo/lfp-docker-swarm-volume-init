package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

const repairLabel = "com.lfp.docker.swarm.volume-init"

type dockerAPI interface {
	Events(context.Context, client.EventsListOptions) client.EventsResult
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	TaskList(context.Context, client.TaskListOptions) (client.TaskListResult, error)
	ContainerCreate(context.Context, client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerWait(context.Context, string, client.ContainerWaitOptions) client.ContainerWaitResult
	ContainerRemove(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
}

type app struct {
	docker   dockerAPI
	config   config
	logger   *slog.Logger
	inFlight map[string]struct{}
	mu       sync.Mutex
}

func newApp(docker dockerAPI, config config, logger *slog.Logger) *app {
	return &app{
		docker:   docker,
		config:   config,
		logger:   logger,
		inFlight: make(map[string]struct{}),
	}
}

func (a *app) run(ctx context.Context) error {
	jobs := make(chan events.Message, a.config.workerCount*4)
	var workers sync.WaitGroup
	for range a.config.workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for event := range jobs {
				if err := a.handleEvent(ctx, event); err != nil && !errors.Is(err, context.Canceled) {
					a.logger.Warn("event processing failed", "event_type", event.Type, "action", event.Action, "error", err)
				}
			}
		}()
	}
	defer func() {
		close(jobs)
		workers.Wait()
	}()

	backoff := a.config.reconnectMin
	since := ""
	for {
		nextSince, receivedEvent, err := a.watch(ctx, since, jobs)
		if nextSince != "" {
			since = nextSince
		}
		if ctx.Err() != nil {
			return nil
		}
		if receivedEvent {
			backoff = a.config.reconnectMin
		}
		if err != nil && !errors.Is(err, io.EOF) {
			a.logger.Warn("Docker event stream disconnected", "error", err, "retry_in", backoff)
		} else {
			a.logger.Warn("Docker event stream closed", "retry_in", backoff)
		}

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff = min(backoff*2, a.config.reconnectMax)
	}
}

func (a *app) watch(ctx context.Context, since string, jobs chan<- events.Message) (string, bool, error) {
	filters := make(client.Filters).Add("type", string(events.ContainerEventType), string(events.VolumeEventType))
	stream := a.docker.Events(ctx, client.EventsListOptions{Since: since, Filters: filters})
	a.logger.Info("Docker event monitoring started", "since", since)

	receivedEvent := false
	for stream.Messages != nil || stream.Err != nil {
		select {
		case <-ctx.Done():
			return since, receivedEvent, ctx.Err()
		case event, ok := <-stream.Messages:
			if !ok {
				stream.Messages = nil
				continue
			}
			receivedEvent = true
			if event.TimeNano > 0 {
				since = time.Unix(0, event.TimeNano+1).UTC().Format(time.RFC3339Nano)
			} else if event.Time > 0 {
				// Second-resolution timestamps are replayed to avoid skipping peers from the same second.
				since = time.Unix(event.Time, 0).UTC().Format(time.RFC3339Nano)
			}
			select {
			case jobs <- event:
			case <-ctx.Done():
				return since, receivedEvent, ctx.Err()
			}
		case err, ok := <-stream.Err:
			if !ok {
				stream.Err = nil
				continue
			}
			return since, receivedEvent, err
		}
	}
	return since, receivedEvent, io.EOF
}

func (a *app) handleEvent(ctx context.Context, event events.Message) error {
	attributes := event.Actor.Attributes
	containerID := strings.TrimSpace(attributes["container"])
	if containerID != "" {
		return a.handleContainer(ctx, containerID)
	}

	taskID := strings.TrimSpace(attributes["com.docker.swarm.task.id"])
	if taskID != "" {
		tasks, err := a.docker.TaskList(ctx, client.TaskListOptions{
			Filters: make(client.Filters).Add("id", taskID),
		})
		if err != nil {
			return fmt.Errorf("list task %s: %w", taskID, err)
		}
		for _, task := range tasks.Items {
			if errorContext, ok := errorContextFromTask(task); ok {
				if err := a.processErrorContext(ctx, errorContext); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if event.Type == events.ContainerEventType && strings.TrimSpace(event.Actor.ID) != "" {
		return a.handleContainer(ctx, event.Actor.ID)
	}
	return nil
}

func (a *app) handleContainer(ctx context.Context, containerID string) error {
	result, err := a.docker.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect container %s: %w", containerID, err)
	}
	if errorContext, ok := errorContextFromContainer(result.Container); ok {
		return a.processErrorContext(ctx, errorContext)
	}
	return nil
}

func (a *app) processErrorContext(ctx context.Context, errorContext errorContext) error {
	failure, ok := parseMountFailure(errorContext.error)
	if !ok || !errorContext.matches(failure) {
		return nil
	}
	if !a.config.hostPaths.allows(failure.hostPath) {
		a.logger.Info("host path rejected by filter", "host_path", failure.hostPath)
		return nil
	}
	if !a.acquire(failure.hostPath) {
		a.logger.Debug("repair already in flight", "host_path", failure.hostPath)
		return nil
	}
	defer a.release(failure.hostPath)

	a.logger.Info("repairing missing local-volume host path", "host_path", failure.hostPath, "image", errorContext.imageID)
	if err := a.repair(ctx, errorContext.imageID, failure.hostPath); err != nil {
		return fmt.Errorf("repair host path %s: %w", failure.hostPath, err)
	}
	a.logger.Info("host path repair completed", "host_path", failure.hostPath)
	return nil
}

func (a *app) repair(ctx context.Context, imageID, hostPath string) (returnErr error) {
	repairCtx, cancel := context.WithTimeout(ctx, a.config.repairTimeout)
	defer cancel()

	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	containerPath := "/volume-init-" + suffix
	created, err := a.docker.ContainerCreate(repairCtx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:      imageID,
			Entrypoint: []string{"/bin/sh"},
			Cmd:        []string{"-c", "true"},
			Labels: map[string]string{
				repairLabel: "true",
			},
		},
		HostConfig: &container.HostConfig{
			Privileged: true,
			AutoRemove: true,
			// Docker's legacy bind syntax creates a missing source directory; --mount does not.
			Binds: []string{hostPath + ":" + containerPath},
		},
	})
	if err != nil {
		return fmt.Errorf("create temporary container: %w", err)
	}
	if created.ID == "" {
		return errors.New("Docker returned an empty temporary container ID")
	}
	defer func() {
		// Cleanup must survive SIGTERM and repair timeouts.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), a.config.cleanupTimeout)
		defer cleanupCancel()
		_, cleanupErr := a.docker.ContainerRemove(cleanupCtx, created.ID, client.ContainerRemoveOptions{Force: true})
		if cleanupErr != nil && !errdefs.IsNotFound(cleanupErr) && !errdefs.IsConflict(cleanupErr) {
			cleanupErr = fmt.Errorf("remove temporary container %s: %w", created.ID, cleanupErr)
			if returnErr == nil {
				returnErr = cleanupErr
			} else {
				a.logger.Warn("temporary container cleanup failed", "container_id", created.ID, "error", cleanupErr)
			}
		}
	}()

	_, err = a.docker.ContainerStart(repairCtx, created.ID, client.ContainerStartOptions{})
	if err != nil {
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "/bin/sh") && strings.Contains(lower, "no such file") {
			return nil
		}
		return fmt.Errorf("start temporary container: %w", err)
	}

	wait := a.docker.ContainerWait(repairCtx, created.ID, client.ContainerWaitOptions{
		Condition: container.WaitConditionNotRunning,
	})
	select {
	case <-repairCtx.Done():
		return repairCtx.Err()
	case err := <-wait.Error:
		if err != nil {
			return fmt.Errorf("wait for temporary container: %w", err)
		}
	case result := <-wait.Result:
		if result.Error != nil {
			return fmt.Errorf("temporary container failed: %s", result.Error.Message)
		}
		if result.StatusCode != 0 {
			return fmt.Errorf("temporary container exited with status %d", result.StatusCode)
		}
	}
	return nil
}

func (a *app) acquire(hostPath string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.inFlight[hostPath]; exists {
		return false
	}
	a.inFlight[hostPath] = struct{}{}
	return true
}

func (a *app) release(hostPath string) {
	a.mu.Lock()
	delete(a.inFlight, hostPath)
	a.mu.Unlock()
}

func randomSuffix() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate temporary path suffix: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
