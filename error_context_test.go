package main

import (
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/swarm"
)

func TestParseMountFailure(t *testing.T) {
	message := "error while mounting volume '/var/lib/docker/volumes/data/_data': failed to mount local volume: mount /mnt/storage/data:/var/lib/docker/volumes/data/_data, data: addr=server: no such file or directory"
	failure, ok := parseMountFailure(message)
	if !ok {
		t.Fatal("expected mount failure to parse")
	}
	if failure.hostPath != "/mnt/storage/data" {
		t.Fatalf("host path = %q", failure.hostPath)
	}
	if failure.destinationPath != "/var/lib/docker/volumes/data/_data" {
		t.Fatalf("destination path = %q", failure.destinationPath)
	}
}

func TestParseMountFailureRejectsUnrelatedErrors(t *testing.T) {
	tests := []string{
		"failed to mount local volume: mount /a:/b, permission denied",
		"no such file or directory",
		"failed to mount local volume: mount relative:/b, no such file or directory",
	}
	for _, message := range tests {
		if _, ok := parseMountFailure(message); ok {
			t.Errorf("unexpected match for %q", message)
		}
	}
}

func TestContainerContextMatchesLocalVolumeDestination(t *testing.T) {
	state := &container.State{Error: "mount error"}
	ctx, ok := errorContextFromContainer(container.InspectResponse{
		Image: "sha256:image",
		State: state,
		Mounts: []container.MountPoint{{
			Driver:      "local",
			Source:      "/var/lib/docker/volumes/data/_data",
			Destination: "/data",
		}},
	})
	if !ok || !ctx.matches(mountFailure{destinationPath: "/var/lib/docker/volumes/data/_data"}) {
		t.Fatal("expected local volume source to match")
	}
}

func TestTaskContextMatchesLocalDriverDevice(t *testing.T) {
	ctx, ok := errorContextFromTask(swarm.Task{
		Status: swarm.TaskStatus{Err: "mount error"},
		Spec: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{
			Image: "registry/image:tag",
			Mounts: []mount.Mount{{
				VolumeOptions: &mount.VolumeOptions{DriverConfig: &mount.Driver{
					Name:    "local",
					Options: map[string]string{"device": "/mnt/storage/data"},
				}},
			}},
		}},
	})
	if !ok || !ctx.matches(mountFailure{hostPath: "/mnt/storage/data"}) {
		t.Fatal("expected local driver device to match")
	}
}
