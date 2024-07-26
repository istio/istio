local g = import 'g.libsonnet';

local row = g.panel.row;

local grid = import 'lib-grid.libsonnet';
local dashboard = import './dashboard.libsonnet';
local panels = import './panels.libsonnet';
local variables = import './variables.libsonnet';
local queries = (import './queries.libsonnet').queries({
  container: 'istio-proxy',
  pod: 'ztunnel-.*',
  component: 'ztunnel',
  app: 'ztunnel',
});

dashboard.new('Istio Mesh Dashboard')
+ g.dashboard.withPanels(
  grid.makeGrid([
    row.new('Global Traffic')
    + row.withPanels([
      panels.timeSeries.statRps('Traffic Volume', queries.globalRequest, 'Total requests in the cluster'),
      panels.timeSeries.statPercent('Success Rate', queries.globalRequestSuccessRate, 'Total success rate of requests in the cluster'),
      panels.timeSeries.statRps('4xxs', queries.globalRequest4xx, 'Total 4xx requests in in the cluster'),
      panels.timeSeries.statRps('5xxs', queries.globalRequest5xx, 'Total 5xx requests in in the cluster'),
    ]),
  ], panelHeight=4)
  + [
    panels.tables.requests('HTTP/gRPC Workloads', queries.httpWorkloads, 'Request information for HTTP services') + {
      gridPos+: {
        h: 16,
        w: 24,
        y: 4,
      },
    },
  ]
)
+ g.dashboard.withUid(std.md5('istio-mesh.json'))
