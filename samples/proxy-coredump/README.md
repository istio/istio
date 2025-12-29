# Enabling Coredumps

If the istio-proxy crashes, it will dump a core file which can be used to diagnose why it crashed.
This is useful when filing a bug report.

However, the proxy runs with a read-only filesystem, so the default core-dumping configuration will generally not enabled
the proxy to dump cores.

Instead, a *per node* `sysctl` can be tuned to change the location of the core dump.
Warning: this impacts all processes on the entire node, not just Istio.

This can be done by running `sysctl -w kernel.core_pattern=/var/lib/istio/data/core.proxy && ulimit -c unlimited` on the node.

To do this for all nodes, a `DaemonSet` is provided.
Run `kubectl apply -f daemonset.yaml` to apply it.
Note: this requires elevated privileges.
