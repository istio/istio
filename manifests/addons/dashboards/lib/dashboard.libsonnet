local g = import 'g.libsonnet';

local variables = import './variables.libsonnet';

{
  new(name):
    g.dashboard.new(name)
    + g.dashboard.graphTooltip.withSharedCrosshair()
    + g.dashboard.withRefresh('15s')
    + g.dashboard.time.withFrom('now-30m')
    + g.dashboard.time.withTo('now')
    + g.dashboard.withVariables([variables.datasource]),
}
