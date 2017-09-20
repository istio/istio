# Istio ServiceGraph

*WARNING WARNING WARNING WARNING*

These services are examples ONLY. This code may change at will, or be removed
entirely without warning. Taking any dependency on this code is done at your own
peril.

## Services

### Servicegraph service

Defined in `servicegraph/cmd/server`, this provides a basic HTTP API for
generating servicegraphs. It exposes the following endpoints:
- `/graph` which provides a JSON serialization of the servicegraph
- `/dotgraph` which provides a dot serialization of the servicegraph
- `/dotviz` which provides a visual representation of the servicegraph

All endpoints take an optional argument of `time_horizon`, which controls the 
timespan to consider for graph generation.

All endpoints also take an optional arugment of `filter_empty=true`, which will
restrict the nodes and edges shown to only those that reflect non-zero traffic
levels during the specified `time_horizon`.

### Demosvc service
Defined in `servicegraph/cmd/demosvc`, this provides a simple HTTP endpoint that
generates prometheus metrics. This can be used to test the servicegraph service.
