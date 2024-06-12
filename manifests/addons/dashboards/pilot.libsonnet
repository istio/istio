local g = import 'g.libsonnet';

local row = g.panel.row;

local grid = import 'lib-grid.libsonnet';
local dashboard = import './dashboard.libsonnet';
local panels = import './panels.libsonnet';
local variables = import './variables.libsonnet';
local queries = (import './queries.libsonnet').queries({
  container: 'discovery',
  pod: 'istiod-.*',
  component: 'pilot',
  app: 'istiod',
});

dashboard.new('Istio Control Plane Dashboard')
+ g.dashboard.withPanels(
  grid.makeGrid([
    row.new('Deployed Versions')
    + row.withPanels([
      panels.timeSeries.simple('Pilot Versions', queries.istioBuild, 'Version number of each running instance'),
    ]),
  ], panelHeight=5)
  + grid.makeGrid([
    row.new('Resource Usage')
    + row.withPanels([
      panels.timeSeries.bytes('Memory Usage', queries.goMemoryUsage, 'Memory usage of each running instance'),
      panels.timeSeries.allocations('Memory Allocations', queries.goAllocations, 'Details about memory allocations'),
      panels.timeSeries.base('CPU Usage', queries.cpuUsage, 'CPU usage of each running instance'),
      panels.timeSeries.base('Goroutines', queries.goroutines, 'Goroutine count for each running instance'),
    ]),
  ], panelHeight=10, startY=1)
  + g.util.grid.makeGrid([
    row.new('Push Information')
    + row.withPanels([
      panels.timeSeries.xdsPushes(
        'XDS Pushes', queries.xdsPushes, |||
          Rate of XDS push operations, by type. This is incremented on a per-proxy basis.
        |||
      ),
      panels.timeSeries.base(
        'Events', queries.pilotEvents, |||
          Size of each xDS push.
        |||
      ),
      panels.timeSeries.base(
        'Connections', queries.xdsConnections, |||
          Total number of XDS connections
        |||
      ),
      panels.timeSeries.base(
        'Push Errors', queries.xdsErrors, |||
          Number of push errors. Many of these are at least potentional fatal and should be explored in-depth via Istiod logs.
          Note: metrics here do not use rate() to avoid missing transition from "No series"; series are not reported if there are no errors at all.
        |||
      ),
      panels.heatmap.base(
        'Push Time', queries.pushTime, |||
          Count of active and pending proxies managed by each instance.
          Pending is expected to converge to zero.
        |||
      ),
      panels.heatmap.bytes(
        'Push Size', queries.pushSize, |||
          Size of each xDS push.
        |||
      ),
    ]),
  ], panelHeight=10, startY=3)
  + grid.makeGrid([
    row.new('Webhooks')
    + row.withPanels([
      panels.timeSeries.simple(
        'Validation', queries.validateWebhook, |||
          Rate of XDS push operations, by type. This is incremented on a per-proxy basis.
        |||
      ),
      panels.timeSeries.simple(
        'Injection', queries.injectionWebhook, |||
          Size of each xDS push.
        |||
      ),
    ]),
  ],  startY=100)
)
+ g.dashboard.withUid(std.md5('pilot-dashboard.json'))
