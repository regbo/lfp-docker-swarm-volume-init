package com.lfp.docker.swarm.volume.init;

import com.github.dockerjava.api.command.InspectContainerResponse;
import com.github.dockerjava.api.model.*;
import lombok.AccessLevel;
import lombok.Getter;
import lombok.experimental.FieldDefaults;
import one.util.streamex.StreamEx;
import org.apache.commons.lang3.StringUtils;
import org.apache.commons.lang3.Validate;

import java.util.Objects;
import java.util.Optional;
import java.util.function.BiPredicate;
import java.util.function.Function;

/**
 * Represents a mount-related error context for a container or task.
 * <p>
 * This class wraps the image ID, error message, and logic to test mount paths
 * (either host paths or container destinations) based on whether they relate to the reported error.
 * </p>
 *
 * <p>
 * It implements {@link BiPredicate} to allow quick filtering using path-based conditions.
 * </p>
 */
@FieldDefaults(makeFinal = true, level = AccessLevel.PRIVATE)
@Getter
public class ErrorContext implements BiPredicate<String, String> {

    /**
     * Creates an {@link ErrorContext} from a container inspection result.
     *
     * @param inspectContainerResponse the container inspection result (may be {@code null})
     * @return an {@link ErrorContext} or {@code null} if no error or no matching mounts were found
     */
    public static ErrorContext from(InspectContainerResponse inspectContainerResponse) {
        if (inspectContainerResponse == null)
            return null;

        var imageId = inspectContainerResponse.getImageId();
        if (StringUtils.isBlank(imageId))
            return null;

        var error = Optional
                .ofNullable(inspectContainerResponse.getState())
                .map(InspectContainerResponse.ContainerState::getError)
                .orElse(null);
        if (StringUtils.isBlank(error))
            return null;

        var mountValues = StreamEx
                .ofNullable(inspectContainerResponse.getMounts())
                .flatCollection(Function.identity())
                .nonNull()
                .flatMap(mount -> {
                    if (!"local".equals(mount.getDriver()))
                        return null;
                    return StreamEx.ofNullable(mount.getDestination())
                            .map(Volume::getPath)
                            .prepend(mount.getSource());
                })
                .filter(StringUtils::isNotBlank)
                .toImmutableList();

        if (mountValues.isEmpty())
            return null;

        return new ErrorContext(imageId, error, (nil, destinationPath) ->
                mountValues.stream().anyMatch(v -> v.equals(destinationPath)));
    }

    /**
     * Creates an {@link ErrorContext} from a Docker Swarm task definition.
     *
     * @param task the Docker task (may be {@code null})
     * @return an {@link ErrorContext} or {@code null} if no error or volume info could be extracted
     */
    public static ErrorContext from(Task task) {
        if (task == null)
            return null;

        var error = Optional
                .ofNullable(task.getStatus())
                .map(TaskStatus::getErr)
                .orElse(null);
        if (StringUtils.isBlank(error))
            return null;

        var containerSpec = Optional.ofNullable(task.getSpec())
                .map(TaskSpec::getContainerSpec)
                .orElse(null);
        if (containerSpec == null)
            return null;

        var imageId = containerSpec.getImage();
        if (StringUtils.isBlank(imageId))
            return null;

        var devices = StreamEx
                .ofNullable(containerSpec.getMounts())
                .flatCollection(Function.identity())
                .nonNull()
                .map(mount -> {
                    var driverConfig = Optional.ofNullable(mount.getVolumeOptions())
                            .map(VolumeOptions::getDriverConfig)
                            .orElse(null);
                    if (driverConfig == null || !"local".equals(driverConfig.getName()))
                        return null;
                    return Optional.ofNullable(driverConfig.getOptions())
                            .map(v -> v.get("device"))
                            .orElse(null);
                })
                .filter(StringUtils::isNotBlank)
                .toImmutableList();

        if (devices.isEmpty())
            return null;

        return new ErrorContext(imageId, error, (hostPath, nil) ->
                devices.stream().anyMatch(v -> v.equals(hostPath)));
    }

    /** The ID of the container image involved in the error. */
    String imageId;

    /** The reported error message from container or task state. */
    String error;

    /** A predicate to match host or container mount paths that relate to the error. */
    BiPredicate<String, String> pathPredicate;

    /**
     * Constructs an {@link ErrorContext} with image ID, error, and mount path predicate.
     *
     * @param imageId        the container image ID
     * @param error          the error string reported by the container/task
     * @param pathPredicate  a bi-predicate that tests host and container paths for relevance to the error
     */
    protected ErrorContext(String imageId, String error, BiPredicate<String, String> pathPredicate) {
        this.imageId = Validate.notBlank(imageId);
        this.error = Validate.notBlank(error);
        this.pathPredicate = Objects.requireNonNull(pathPredicate);
    }

    /**
     * Tests whether the given host and container mount path pair matches this error context.
     *
     * @param hostPath         the host path
     * @param destinationPath  the container path
     * @return true if the error context applies to this mount, false otherwise
     */
    @Override
    public boolean test(String hostPath, String destinationPath) {
        return this.pathPredicate.test(hostPath, destinationPath);
    }
}

