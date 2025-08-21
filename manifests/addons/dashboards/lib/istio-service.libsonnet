local g = import 'g.libsonnet';

// This file provides consistent UIDs for dashboard links to the service dashboard

{
  // Helper to get the UID of the Service dashboard for dashboard linking
  uid:: std.md5('istio-service-dashboard.json'),

  // Create a data link to the Service dashboard with appropriate variables filled in
  dataLink(title='View Service'):: 
    g.panel.link.new(title)
    + g.panel.link.withUrl('/d/' + $.uid + '?${vars}')
    + g.panel.link.withTargetBlank(true),

  // Create a dashboard link to the Service dashboard
  dashboardLink:: 
    g.dashboard.link.dashboards.new('Service Dashboard', [$.uid])
    + g.dashboard.link.dashboards.options.withAsDropdown(false)
    + g.dashboard.link.dashboards.options.withIncludeVars(true)
    + g.dashboard.link.dashboards.options.withKeepTime(true)
}
