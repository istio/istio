package log

import "istio.io/istio/pkg/log"

var IngressLog = log.RegisterScope("ingress", "Multi-Cluster Ingress process.")
