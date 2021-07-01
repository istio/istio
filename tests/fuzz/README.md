# Istio fuzzing

Istio has a series of fuzzers that run continuously through OSS-fuzz.

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
