# glog

This repo contains a package that exposes an API subset of the [glog](https://github.com/golang/glog) package.
All logging state delivered to this package is shunted to the global [zap logger](https://github.com/uber-go/zap).

Istio is built on top of zap logger. We depend on some downstream components that use glog for logging.
This package makes it so we can intercept the calls to glog and redirect them to zap and thus produce
a consistent log for our processes.
