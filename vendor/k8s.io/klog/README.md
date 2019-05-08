# klog

This repo contains a package that exposes an API subset of the klog package. All logging state delivered to this package is shunted to the global zap logger.

Istio is built on top of zap logger. We depend on some downstream components that use klog for logging. This package makes it so we can intercept the calls to klog and redirect them to zap and thus produce a consistent log for our processes.
