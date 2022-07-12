# Istio fuzzing

Istio has a series of fuzzers that run continuously through OSS-fuzz.

## Native fuzzers

While many jobs are still using the old [go-fuzz](https://github.com/dvyukov/go-fuzz) style fuzzers, using [Go 1.18 native fuzzing](https://go.dev/doc/fuzz/) is preferred.
These should be written alongside standard test packages.
Currently, these cannot be in `<pkg>_test` packages; instead move them to a file under `<pkg>`.

Fuzz jobs will be run in unit test mode automatically (i.e. run once) and as part of OSS-fuzz.

## Local testing

To run the fuzzers using `Dockerfile.fuzz`, follow these steps:

```bash
git clone https://github.com/istio/istio
```

```bash
cd istio
```

```bash
mv tests/fuzz/Dockerfile.fuzz ./
```

```bash
sudo docker build -t istio-fuzz -f Dockerfile.fuzz .
```

Next, get a shell in the container:

```bash
sudo docker run -it istio-fuzz
```

At this point, you can navigate to `tests/fuzz` and build any of the fuzzers:

```bash
cd $PATH_TO_FUZZER
go-fuzz-build -libfuzzer -func=FUZZ_NAME && \
clang -fsanitize=fuzzer PACKAGE_NAME.a -o fuzzer
```

If you encounter any errors when linking with `PACKAGE_NAME.a`, simply `ls` after running `go-fuzz-build...`, and you will see the archive to link with.

If everything goes well until this point, you can run the fuzzer:

```bash
./fuzzer
```
