local g = import 'g.libsonnet';
local ztunnel = import 'ztunnel.libsonnet';

// Example of how to add a dashboard link to the Ztunnel dashboard
// This can be used in any dashboard that needs to link to the Ztunnel dashboard

local dashboard = import './dashboard.libsonnet';

dashboard.new('Example Dashboard with Ztunnel Link')
+ g.dashboard.withLinks([
  // Add link to Ztunnel dashboard using its UID
  g.dashboard.link.dashboards.new('Ztunnel Dashboard', [std.md5('ztunnel.json')])
  + g.dashboard.link.dashboards.options.withAsDropdown(false)
  + g.dashboard.link.dashboards.options.withIncludeVars(true)
  + g.dashboard.link.dashboards.options.withKeepTime(true)
])
