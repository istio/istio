# Local Envoy integration testing

Parameters:
- `-p=1 -parallel=1 -timeout 30m` recommended for slow Asan/Tsan builds.
- `ASAN=true`, `TSAN=true` for Asan/Tsan builds.
- `ENVOY_PATH` path to the Envoy binary when not using the default.
- `ENVOY_DEBUG=debug` to increase logging.
