# SDS Local Integration Test

## Test Setup

This test starts an Envoy proxy, a SDS server, a test backend and a dummy CA server. All of these components are running in individual processes.

The Envoy has two sets of listener and cluster configurations, which mimics two sidecars. An HTTP client sends a request and should expect response sent from the test backend.

```bash
                                  +---------+
                                  |   CA    |
                                  |  server |
                                  +----+----+
                                       |
                                  +----+----+
                                  |   SDS   |
                             +----+  server +----+
                             |    |         |    |
                             |    +---------+    |
+--------+    +--------------+-------------------+-------------+    +---------+
| HTTP   |--->| outbound-->outbound<--mTLS-->inbound-->inbound |--->| test    |
| client |    | listener   cluster           listener  cluster |    | backend |
+--------+    +------------------------------------------------+    +---------+

```

Outbound cluster and inbound listener contain SDS configuration, with separate resource name. SDS server generates CSR requests for each of them and sends CSR to CA for cert signing.