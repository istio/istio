# als-sink

This is a dummy implementation of [envoy als service](https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/accesslog/v3/als.proto) written to be used in the telemetry(metadata-exchange) integration tests. It exposes als streaming service on port `9001` and admin endpoint at `16000`. It keeps stores metadata information received from the envoys into an in-memory cache, which can be dumped using `/metadata` at admin port. cache can be cleared at runtime using `/reset` at admin endpoint.

Integration tests dump in-memory cache and unmarshal to go structure to verify the test objective related to metadata exchange. Any change to cache structure in the package will require corresponding changes in the integation tests.