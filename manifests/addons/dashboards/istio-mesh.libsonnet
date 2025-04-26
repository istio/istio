local g = import 'g.libsonnet';

local row = g.panel.row;

local grid = import 'lib-grid.libsonnet';
local dashboard = import './dashboard.libsonnet';
local panels = import './panels.libsonnet';
local variables = import './variables.libsonnet';

local serviceDashboard  = import './istio-service.libsonnet';
local workloadDashboard = import './istio-workload.libsonnet';

local queries = (import './queries.libsonnet').queries({
  container: '',
  pod: '',
  component: '',
  app: '',
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
  ], panelHeight=5)
  + [

    panels.tables.requests('HTTP/gRPC Workloads', queries.httpWorkloads, 'Request information for HTTP services') + {
      gridPos+: {
        h: 16,
        w: 24,
        y: 10,
      },
    },
    panels.tables.tcpRequests('TCP Workloads', queries.tcpWorkloads, 'Bytes sent and recieived information for TCP services') + {
      gridPos+: {
        h: 16,
        w: 24,
        y: 16+10,
      },
    },
  ]
  +
  grid.makeGrid([
    row.new('Istio Component Versions')
    + row.withPanels([
      panels.timeSeries.simple('Istio Component Versions', queries.allIstioBuild, 'Version number of each running instance'),
    ]),
  ], startY=16+10+16)
)
+ g.dashboard.withLinks([
    serviceDashboard.dashboardLink,
    workloadDashboard.dashboardLink,
  ])
+ g.dashboard.withUid(std.md5('istio-mesh.json'))
