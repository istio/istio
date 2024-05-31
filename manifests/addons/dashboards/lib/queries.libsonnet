local g = import './g.libsonnet';
local q = g.query.prometheus;

local variables = import './variables.libsonnet';

local containerLabels = 'container="istio-proxy", pod=~"ztunnel-.*"';
local podLabels = 'pod=~"ztunnel-.*"';

{
  query(legend, query):
    q.new(
      '$' + variables.datasource.name,
      std.rstripChars(query % { containerLabels: containerLabels, podLabels: podLabels }, '\n')
    )
    + q.withLegendFormat(legend),

  istioBuild:
    self.query('Version ({{tag}})', 'sum(istio_build{component="ztunnel"}) by (tag)'),

  cpuUsage:
    self.query(
      'Container ({{pod}})',
      |||
        sum by (pod) (
          irate(
            container_cpu_usage_seconds_total{%(containerLabels)s}
          [$__rate_interval])
        )
      |||
    ),

  memUsage:
    self.query(
      'Container ({{pod}})',
      |||
        sum by (pod) (
          container_memory_working_set_bytes{%(containerLabels)s}
        )
      |||
    ),

  connections:
    [
      self.query(
        'Opened ({{pod}})',
        |||
          sum by (pod) (
            rate(
              istio_tcp_connections_opened_total{%(podLabels)s}
            [$__rate_interval])
          )
        |||
      ),
      self.query(
        'Closed ({{pod}})',
        |||
          -sum by (pod) (
            rate(
              istio_tcp_connections_closed_total{%(podLabels)s}
            [$__rate_interval])
          )
        |||
      ),
    ],

  bytes:
    [
      self.query(
        'Sent ({{pod}})',
        |||
          sum by (pod) (
            rate(
              istio_tcp_sent_bytes_total{%(podLabels)s}
            [$__rate_interval])
          )
        |||
      ),
      self.query(
        'Received ({{pod}})',
        |||
          sum by (pod) (
            rate(
              istio_tcp_received_bytes_total{%(podLabels)s}
            [$__rate_interval])
          )
        |||
      ),
    ],

  dns:
    self.query(
      'Request ({{pod}})',
      |||
        sum by (pod) (
          rate(
            istio_dns_requests_total{%(podLabels)s}
          [$__rate_interval])
        )
      |||
    ),

  xdsConnections:
    self.query(
      'XDS Connection Terminations ({{pod}})',
      |||
        sum by (pod) (
          rate(
            istio_xds_connection_terminations_total{%(podLabels)s}
          [$__rate_interval])
        )
      |||
    ),

  workloadManager: [
    self.query(
      'Active Proxies ({{pod}})',
      |||
        sum by (pod) (workload_manager_active_proxy_count{%(podLabels)s})
      |||
    ),
    self.query(
      'Pending Proxies ({{pod}})',
      |||
        sum by (pod) (workload_manager_pending_proxy_count{%(podLabels)s})
      |||
    ),
  ],
}
