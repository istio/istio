# Istio fuzzing

Istio has a series of fuzzers that run continuously through OSS-fuzz.

## Native fuzzers

While many jobs are still using the old [go-fuzz](https://github.com/dvyukov/go-fuzz) style fuzzers, using [Go 1.18 native fuzzing](https://go.dev/doc/fuzz/) is preferred.
These should be written alongside standard test packages.
Currently, these cannot be in `<pkg>_test` packages; instead move them to a file under `<pkg>`.

Fuzz jobs will be run in unit test mode automatically (i.e. run once) and as part of OSS-fuzz.

## Local testing

To run the fuzzers, follow these steps:

```bash
git clone --depth=1 https://github.com/google/oss-fuzz.git
cd oss-fuzz
python infra/helper.py build_image istio
python infra/helper.py build_fuzzers istio ~/go/src/istio.io/istio
```

Reproduce failure:

```bash
python infra/helper.py reproduce istio FuzzX ~/Downloads/clusterfuzz-testcase-minimized-FuzzX-1234
```

Run:

```bash
python infra/helper.py run_fuzzer istio FuzzValidateMeshConfig
```
