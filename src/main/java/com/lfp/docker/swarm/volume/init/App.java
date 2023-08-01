package com.lfp.docker.swarm.volume.init;

import com.github.dockerjava.api.DockerClient;
import com.github.dockerjava.api.command.InspectContainerResponse;
import com.github.dockerjava.api.exception.BadRequestException;
import com.github.dockerjava.api.exception.ConflictException;
import com.github.dockerjava.api.exception.NotFoundException;
import com.github.dockerjava.api.model.*;
import com.github.dockerjava.core.DefaultDockerClientConfig;
import com.github.dockerjava.core.DockerClientImpl;
import com.github.dockerjava.httpclient5.ApacheDockerHttpClient;
import com.machinezoo.noexception.throwing.ThrowingRunnable;
import com.machinezoo.noexception.throwing.ThrowingSupplier;
import lombok.AccessLevel;
import lombok.SneakyThrows;
import lombok.experimental.FieldDefaults;
import lombok.extern.slf4j.Slf4j;
import net.robinfriedli.threadpool.ThreadPool;
import one.util.streamex.EntryStream;
import one.util.streamex.StreamEx;
import org.apache.commons.lang3.RandomStringUtils;
import org.apache.commons.lang3.StringUtils;
import org.apache.commons.lang3.Validate;
import reactor.core.Disposable;
import reactor.core.Disposables;

import java.util.*;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.Future;
import java.util.function.Function;
import java.util.function.Predicate;
import java.util.function.Supplier;
import java.util.stream.Stream;

@FieldDefaults(makeFinal = true, level = AccessLevel.PRIVATE)
@Slf4j
public class App implements ThrowingRunnable {
    private static final String HOST_PATH_FILTER_PROPERTY_NAME = "HOST_PATH_FILTER";
    Map<String, CompletableFuture<?>> hostPathFutures = new ConcurrentHashMap<>();
    ThreadPool threadPool;
    DockerClient dockerClient;
    Disposable.Composite onComplete;
    Predicate<String> hostPathFilter;


    public App() {
        this.threadPool = ThreadPool.Builder.create()
                                            .setCoreSize(0)
                                            .setMaxSize(Runtime.getRuntime().availableProcessors())
                                            .build();
        var config = DefaultDockerClientConfig.createDefaultConfigBuilder().build();
        var httpClient = new ApacheDockerHttpClient.Builder()
                .dockerHost(config.getDockerHost())
                .sslConfig(config.getSSLConfig())
                .build();
        this.dockerClient = DockerClientImpl.getInstance(config, httpClient);
        this.onComplete = Disposables.composite();
        this.hostPathFilter = streamPropertyValues(HOST_PATH_FILTER_PROPERTY_NAME)
                .mapToEntry(App::toHostPathFilter).peekKeys(hostPathFilter -> {
                    log.info("adding host path filter:{}", hostPathFilter);
                }).values()
                .reduce(Predicate::or)
                .orElse(v -> true);
    }

    @Override
    public void run() throws Exception {
        try {
            var callbackFuture = this.registerFuture(ResultCallbackFuture
                    .<Event>builder()
                    .onStart(nil -> log.info("event monitoring started"))
                    .onNext(this::onEvent)
                    .build());
            dockerClient.eventsCmd().withEventTypeFilter("volume", "container").exec(callbackFuture);
            callbackFuture.join();
        } finally {
            this.onComplete.dispose();
        }
    }


    private void onEvent(Event event) {
        var attributes = Optional
                .ofNullable(event.getActor())
                .map(EventActor::getAttributes).orElse(null);
        if (attributes == null)
            return;
        CompletableFuture<StreamEx<ErrorContext>> errorContextStreamFuture;
        var containerId = attributes.get("container");
        if (StringUtils.isNotBlank(containerId)) {
            errorContextStreamFuture = CompletableFuture.supplyAsync(() -> {
                var inspectContainerCommand = this.dockerClient.inspectContainerCmd(containerId);
                var inspectContainerResponse = get(inspectContainerCommand::exec, NotFoundException.class::isInstance);
                var errorContext = ErrorContext.from(inspectContainerResponse);
                return StreamEx.ofNullable(errorContext);
            }, this.threadPool);
        } else {
            var taskId = attributes.get("com.docker.swarm.task.id");
            if (StringUtils.isBlank(taskId))
                return;
            errorContextStreamFuture = CompletableFuture.supplyAsync(() -> {
                var tasks = this.dockerClient
                        .listTasksCmd()
                        .withIdFilter(taskId)
                        .exec();
                return StreamEx.of(tasks).map(ErrorContext::from);
            }, this.threadPool);
        }
        this.registerFuture(errorContextStreamFuture.thenCompose(errorContexts -> {
            var errorContextFutures = errorContexts.nonNull().map(errorContext -> {
                var errorContextFuture = CompletableFuture.runAsync(() -> this.processErrorContext(event, errorContext), this.threadPool);
                return registerFuture(errorContextFuture);
            }).toArray(CompletableFuture.class);
            return CompletableFuture.allOf(errorContextFutures);
        }));
        ;
    }


    private void processErrorContext(Event event, ErrorContext errorContext) {
        var mountEntry = this.parseMountEntry(errorContext.getError()).orElse(null);
        if (mountEntry == null)
            return;
        var hostPath = mountEntry.getKey();
        var destinationPath = mountEntry.getValue();
        if (!errorContext.test(hostPath, destinationPath)) return;
        var started = new boolean[]{false};
        var hostPathFuture = this.hostPathFutures.computeIfAbsent(hostPath, nil -> {
            var future = this.processHostPath(event, errorContext.getImageId(), hostPath);
            started[0] = true;
            return future;
        });
        if (!started[0])
            return;
        hostPathFuture.whenComplete((v, t) -> this.hostPathFutures.remove(hostPath));
    }


    private CompletableFuture<Void> processHostPath(Event event, String imageId, String hostPath) {
        var summary = String.format("hostPath:%s imageId:%s", hostPath, imageId);
        log.info("creating container - {}", summary);
        var containerPath = "/v_" + RandomStringUtils.randomAlphanumeric(16);
        var hostConfig = new HostConfig()
                .withPrivileged(true)
                .withAutoRemove(true)
                .withBinds(Bind.parse(String.format("%s:%s", hostPath, containerPath)));
        var containerId = this.dockerClient
                .createContainerCmd(imageId)
                .withHostConfig(hostConfig)
                .withEntrypoint("/bin/sh")
                .withCmd("-c", String.format("echo '%s'", summary))
                .exec().getId();
        Validate.notBlank(containerId);
        var hostPathFuture = CompletableFuture.supplyAsync(() -> {
            var command = this.dockerClient.startContainerCmd(containerId);
            return run(command::exec,
                    t -> t instanceof BadRequestException && StringUtils.containsIgnoreCase(t.getMessage(), "/bin/sh: no such file"));
        }, this.threadPool).thenCompose(started -> {
            if (!started)
                return CompletableFuture.completedStage(null);
            var callbackFuture = ResultCallbackFuture.<Frame>of(v -> log.info(Optional.ofNullable(v).map(Objects::toString).orElse(null)));
            var command = this.dockerClient.logContainerCmd(containerId).withStdOut(true).withStdErr(true);
            command.exec(callbackFuture);
            return callbackFuture;
        }).exceptionallyCompose(t -> {
            if (t instanceof NotFoundException)
                return CompletableFuture.completedStage(null);
            return CompletableFuture.failedStage(t);
        }).whenComplete((v, t) -> {
            var command = this.dockerClient.removeContainerCmd(containerId);
            run(command::exec, NotFoundException.class::isInstance, ConflictException.class::isInstance);
        });
        return registerFuture(hostPathFuture);
    }


    private <F extends Future<?> & CompletionStage<?>> F registerFuture(F future) {
        if (!future.isDone()) {
            future.whenComplete((v, t) -> {
                if (t != null)
                    log.warn("async error", t);
            });
            Disposable disposable = () -> future.cancel(true);
            if (this.onComplete.add(disposable))
                future.whenComplete((v, t) -> this.onComplete.remove(disposable));
        }
        return future;
    }


    private Optional<Map.Entry<String, String>> parseMountEntry(String error) {
        if (!StringUtils.containsIgnoreCase(error, "no such file or directory"))
            return Optional.empty();
        var str = StringUtils.substringAfter(error, "failed to mount local volume: mount ");
        str = StringUtils.substringBefore(str, ",");
        var parts = str.split(":", 3);
        if (parts.length != 2)
            return Optional.empty();
        var hostPath = StringUtils.trimToNull(parts[0]);
        if (hostPath == null) return Optional.empty();
        if (!this.hostPathFilter.test(hostPath)) {
            log.info("host path does not match filter:{}", hostPath);
            return Optional.empty();
        }
        var containerPath = StringUtils.trimToNull(parts[1]);
        if (containerPath == null) return Optional.empty();
        return Optional.of(Map.entry(hostPath, containerPath));
    }


    private static Predicate<String> toHostPathFilter(String path) {
        Function<String, String> normalize = value -> {
            if (value == null)
                return "/";
            return StringUtils.endsWith(value, "/") ? value : value + "/";
        };
        var normalizedPath = normalize.apply(path);
        return value -> {
            return normalize.apply(value).startsWith(normalizedPath);
        };
    }

    private static StreamEx<String> getMountValues(InspectContainerResponse inspectContainerResponse) {
        var mounts = Optional.ofNullable(inspectContainerResponse).map(InspectContainerResponse::getMounts).orElse(null);
        if (mounts == null) return StreamEx.empty();
        return StreamEx
                .of(mounts)
                .filter(v -> "local".equals(v.getDriver())).flatMap(mount -> {
                    var values = StreamEx.ofNullable(mount.getDestination()).map(Volume::getPath).prepend(mount.getSource());
                    return values.filter(StringUtils::isNotBlank);
                });
    }

    private static StreamEx<String> streamPropertyValues(String key) {
        if (StringUtils.isBlank(key))
            return StreamEx.empty();
        var valueStream = StreamEx.<Supplier<Stream<String>>>of(() -> StreamEx.ofNullable(System.getenv(key)),
                () -> StreamEx.ofNullable(System.getProperty(key)),
                () -> EntryStream.of(System.getenv()).filterKeys(key::equalsIgnoreCase).values(),
                () -> {
                    var properties = System.getProperties();
                    return StreamEx
                            .ofNullable(properties)
                            .flatCollection(Properties::stringPropertyNames)
                            .filter(key::equalsIgnoreCase)
                            .map(properties::getProperty);
                });
        return valueStream.flatMap(Supplier::get).filter(StringUtils::isNotBlank).distinct();
    }


    @SafeVarargs
    private static boolean run(ThrowingRunnable runnable, Predicate<Throwable>... errorFilters) {
        var result = get(runnable == null ? null : () -> {
            runnable.run();
            return true;
        }, errorFilters);
        return Boolean.TRUE.equals(result);
    }

    @SneakyThrows
    @SafeVarargs
    private static <U> U get(ThrowingSupplier<U> supplier, Predicate<Throwable>... errorFilters) {
        Objects.requireNonNull(supplier);
        try {
            return supplier.get();
        } catch (Throwable t) {
            if (Arrays.stream(errorFilters).anyMatch(v -> v.test(t)))
                return null;
            throw t;
        }
    }


    public static void main(String[] args) throws Exception {
        new App().run();
    }


}
