# Docker Swarm volume initializer

`lfp-docker-swarm-volume-init` repairs a narrow Docker Swarm failure mode: a task cannot start because the host directory backing a local volume does not exist on the selected node.

The daemon watches local Docker container and volume events. For a recognized `failed to mount local volume` error, it validates the path against the configured allowlist and creates a labeled, short-lived privileged container with a bind mount. Docker creates the missing source directory, the temporary container is removed, and Swarm can retry the task.

## Behavior

- Watches container and volume events and reconnects with bounded backoff.
- Handles both ordinary container state and Swarm task failures.
- Processes only local-volume errors containing `no such file or directory`.
- Requires the parsed paths to match the failed container or task definition.
- Deduplicates concurrent repairs for the same host path.
- Times out repairs and combines Docker auto-removal with explicit cleanup during shutdown.
- Labels repair containers with `com.lfp.docker.swarm.volume-init=true`.

Like the Java implementation, the daemon only creates a missing directory on the Docker node where it is running. It does not change ownership or permissions. Run an initializer task on every node whose local paths it must repair; a manager-only deployment cannot repair worker-local bind paths.

## Configuration

`HOST_PATH_FILTER` is an optional comma-separated allowlist of absolute Linux path roots. Matching respects path boundaries: `/mnt/media` allows `/mnt/media` and `/mnt/media/movies`, but not `/mnt/media-library`.

If `HOST_PATH_FILTER` is empty, every absolute host path is allowed. Configure an explicit allowlist in production.

The official Moby client also reads the standard Docker variables, including `DOCKER_HOST`, `DOCKER_API_VERSION`, `DOCKER_TLS_VERIFY`, and `DOCKER_CERT_PATH`.

## Run

The daemon needs Docker socket access and creates privileged repair containers. Docker socket access is host-root-equivalent; only run the image on trusted Swarm nodes.

```yaml
services:
  volume-init:
    image: ghcr.io/regbo/lfp-docker-swarm-volume-init@sha256:<digest>
    environment:
      HOST_PATH_FILTER: /mnt/media,/mnt/storage
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    deploy:
      mode: global
      placement:
        constraints:
          - node.role == manager
```

Deploy by digest so every node runs the exact image tested by CI.

## CI and releases

The [CI and deploy workflow](.github/workflows/ci-deploy.yml) runs race-enabled tests and `go vet` for pull requests. A push to `main`, or a manual workflow dispatch, also builds and publishes `linux/amd64` and `linux/arm64` images to:

```text
ghcr.io/regbo/lfp-docker-swarm-volume-init
```

Every successful publish advances the patch version and creates the corresponding annotated Git tag. With no existing release tags, the first deployment is `v1.0.0`; subsequent deployments are `v1.0.1`, `v1.0.2`, and so on.

Each publish produces these image tags:

- The semantic version, such as `1.0.2`
- `latest`
- The full source commit, prefixed with `sha-`

The workflow uses the repository-scoped `GITHUB_TOKEN`; no registry password is required. It also publishes provenance, an SBOM, an artifact attestation, and the immutable digest in the workflow summary.

To request a manual deployment from PowerShell:

```powershell
./deploy.ps1
```

## Development

Requirements: Go 1.24+ and Docker with Buildx.

```sh
go test -race ./...
go vet ./...
go build ./...
docker buildx build --platform linux/amd64,linux/arm64 .
```
