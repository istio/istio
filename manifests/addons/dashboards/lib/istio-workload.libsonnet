local g = import 'g.libsonnet';

// This file provides consistent UIDs for dashboard links to the workload dashboard

{
  // Helper to get the UID of the Workload dashboard for dashboard linking
  uid:: std.md5('istio-workload-dashboard.json'),

  // Create a data link to the Workload dashboard with appropriate variables filled in
  dataLink(title='View Workload'):: 
    g.panel.link.new(title)
    + g.panel.link.withUrl('/d/' + $.uid + '?${vars}')
    + g.panel.link.withTargetBlank(true),

  // Create a dashboard link to the Workload dashboard
  dashboardLink:: 
    g.dashboard.link.dashboards.new('Workload Dashboard', [$.uid])
    + g.dashboard.link.dashboards.options.withAsDropdown(false)
    + g.dashboard.link.dashboards.options.withIncludeVars(true)
    + g.dashboard.link.dashboards.options.withKeepTime(true)
}
