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

@FieldDefaults(makeFinal = true, level = AccessLevel.PRIVATE)
@Builder
public class ResultCallbackFuture<T> extends CompletableFuture<Void> implements ResultCallback<T> {

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
