package com.lfp.docker.swarm.volume.init;

import com.github.dockerjava.api.async.ResultCallback;
import lombok.AccessLevel;
import lombok.Builder;
import lombok.experimental.FieldDefaults;

import java.io.Closeable;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import java.util.function.Consumer;
import java.util.function.Function;

/**
 * A CompletableFuture-based implementation of {@link com.github.dockerjava.api.async.ResultCallback}
 * that allows processing streamed Docker results using simple lambda consumers.
 *
 * <p>This class bridges callback-style event streams with future-style async control,
 * completing the future when the stream ends or errors out.</p>
 *
 * <p>Example usage:
 * <pre>{@code
 * ResultCallbackFuture<Event> callback = ResultCallbackFuture.<Event>builder()
 *     .onStart(start -> log.info("Started"))
 *     .onNext(event -> handleEvent(event))
 *     .build();
 *
 * dockerClient.eventsCmd().exec(callback);
 * callback.join(); // blocks until complete or error
 * }</pre>
 *
 * @param <T> The type of streamed result object handled
 */
@FieldDefaults(makeFinal = true, level = AccessLevel.PRIVATE)
@Builder
public class ResultCallbackFuture<T> extends CompletableFuture<Void> implements ResultCallback<T> {

    /**
     * Creates a simple {@link ResultCallbackFuture} with an {@code onNext} consumer.
     *
     * @param onNext Consumer to handle each streamed object
     * @param <T>    Type of streamed data
     * @return a new ResultCallbackFuture
     */
    public static <T> ResultCallbackFuture<T> of(Consumer<T> onNext) {
        return ResultCallbackFuture.<T>builder().onNext(onNext).build();
    }

    Consumer<Closeable> onStart;
    Consumer<T> onNext;

    @Override
    public void onStart(Closeable closeable) {
        Optional.ofNullable(this.onStart).ifPresent(v -> v.accept(closeable));
    }

    @Override
    public void onNext(T object) {
        Optional.ofNullable(this.onNext).ifPresent(v -> v.accept(object));
    }

    @Override
    public void onError(Throwable throwable) {
        super.completeExceptionally(throwable);
    }

    @Override
    public void onComplete() {
        super.complete(null);
    }

    @Override
    public void close() {
        super.cancel(true);
    }
}
