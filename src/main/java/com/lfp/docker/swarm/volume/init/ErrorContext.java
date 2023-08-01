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

@FieldDefaults(makeFinal = true, level = AccessLevel.PRIVATE)
@Getter
public class ErrorContext implements BiPredicate<String, String> {

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
                    return StreamEx.ofNullable(mount.getDestination()).map(Volume::getPath).prepend(mount.getSource());
                }).filter(StringUtils::isNotBlank).toImmutableList();
        if (mountValues.isEmpty())
            return null;
        return new ErrorContext(imageId, error, (nil, destinationPath) -> {
            return mountValues.stream().anyMatch(v -> v.equals(destinationPath));
        });
    }

    public static ErrorContext from(Task task) {
        if (task == null)
            return null;
        var error = Optional
                .ofNullable(task.getStatus())
                .map(TaskStatus::getErr)
                .orElse(null);
        if (StringUtils.isBlank(error))
            return null;
        var containerSpec = Optional.ofNullable(task.getSpec()).map(TaskSpec::getContainerSpec).orElse(null);
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
                    var driverConfig = Optional.ofNullable(mount.getVolumeOptions()).map(VolumeOptions::getDriverConfig).orElse(null);
                    if (driverConfig == null || !"local".equals(driverConfig.getName()))
                        return null;
                    return Optional.ofNullable(driverConfig.getOptions()).map(v -> v.get("device")).orElse(null);
                }).filter(StringUtils::isNotBlank).toImmutableList();
        if (devices.isEmpty())
            return null;
        return new ErrorContext(imageId, error, (hostPath, nil) -> {
            return devices.stream().anyMatch(v -> v.equals(hostPath));
        });
    }

    String imageId;

    String error;

    BiPredicate<String, String> pathPredicate;

    protected ErrorContext(String imageId, String error, BiPredicate<String, String> pathPredicate) {
        this.imageId = Validate.notBlank(imageId);
        this.error = Validate.notBlank(error);
        this.pathPredicate = Objects.requireNonNull(pathPredicate);
    }


    @Override
    public boolean test(String hostPath, String destinationPath) {
        return this.pathPredicate.test(hostPath, destinationPath);
    }
}
