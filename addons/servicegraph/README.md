# Istio Servicegraph

Servicegraph is a small app that generates and visualizes graph
representations of your Istio service mesh. Servicegraph is dependent
on the
[Prometheus](https://istio.io/docs/tasks/telemetry/querying-metrics.html)
addon and the standard metrics configuration. The documentation for
deploying and using Servicegraph is
[here](https://istio.io/docs/tasks/telemetry/servicegraph.html).

## Visualizations

- `/force/forcegraph.html` is an interactive
  [D3.js](https://d3js.org/) visualization.

- `/dotviz` is a static [Graphviz](https://www.graphviz.org/)
  visualization.

## Serializations

- `/dotgraph` provides a
  [DOT](https://en.wikipedia.org/wiki/DOT_(graph_description_language))
  serialization.

- `/d3graph` provides a JSON serialization for D3 visualization.

- `/graph` provides a JSON serialization.

## Query Parameters

All endpoints take these query parameters:

- `time_horizon` controls the timespan to consider for graph
  generation. Format is a number plus a time unit. Example `15s` or
  `1m`. Default is `5m`.

- `filter_empty=true` will restrict the nodes and edges shown to only
  those that reflect non-zero traffic levels during the specified
  `time_horizon`. Deafult is `false`.
  
- `destination_namespace` will filter the nodes and edges show to only 
  those where the destination workload is in the given namespace.
  
- `destination_workload` will filter the nodes and edges show to only 
  those where the destination workload is with the given name.

- `source_namespace` will filter the nodes and edges show to only 
  those where the source workload is in the given namespace. 

- `source_workload` will filter the nodes and edges show to only 
  those where the source workload is with the given name.


# Demosvc service
Defined in `servicegraph/cmd/demosvc`, this provides a simple HTTP
endpoint that generates Prometheus metrics. This can be used to test
the servicegraph service.
