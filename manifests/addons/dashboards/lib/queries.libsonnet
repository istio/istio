local g = import './g.libsonnet';
local q = g.query.prometheus;

local variables = import './variables.libsonnet';

{
  queries(names):
    local containerLabels = 'container="%(container)s", pod=~"%(pod)s"' % names;
    local appLabels = 'app="%(app)s"' % names;
    local podLabels = 'pod=~"%(pod)s"' % names;
    {
      query(legend, query):
        q.new(
          '$' + variables.datasource.name,
          std.rstripChars(query % { containerLabels: containerLabels, podLabels: podLabels, appLabels: appLabels }, '\n')
        )
        + q.withLegendFormat(legend),

      istioBuild:
        self.query('Version ({{tag}})', 'sum(istio_build{component="%s"}) by (tag)' % names.component),

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

      goMemoryUsage: [
        self.query(
          'Container ({{pod}})',
          |||
            sum by (pod) (
              container_memory_working_set_bytes{%(containerLabels)s}
            )
          |||
        ),
        self.query(
          'Stack ({{pod}})',
          |||
            sum by (pod) (
              go_memstats_stack_inuse_bytes{%(appLabels)s}
            )
          |||
        ),
        self.query(
          'Heap (In Use) ({{pod}})',
          |||
            sum by (pod) (
              go_memstats_heap_inuse_bytes{%(appLabels)s}
            )
          |||
        ),
        self.query(
          'Heap (Allocated) ({{pod}})',
          |||
            sum by (pod) (
              go_memstats_heap_alloc_bytes{%(appLabels)s}
            )
          |||
        ),
      ],

      goAllocations: [
        self.query(
          'Bytes ({{pod}})',
          |||
            sum by (pod) (
              rate(
                go_memstats_alloc_bytes_total{%(appLabels)s}
              [$__rate_interval])
            )
          |||
        ),
        self.query(
          'Objects ({{pod}})',
          |||
            sum by (pod) (
              rate(
                go_memstats_mallocs_total{%(appLabels)s}
              [$__rate_interval])
            )
          |||
        ),
      ],

      goroutines:
        self.query(
          'Goroutines ({{pod}})',
          |||
            sum by (pod) (
              go_goroutines{%(appLabels)s}
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

      ztunnelXdsConnections:
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

      xdsPushes:
        self.query(
          '{{type}}',
          |||
            sum by (type) (
              irate(
                pilot_xds_pushes{}
              [$__rate_interval])
            )
          |||
        ),

      xdsErrors: [
        self.query(
          'Rejected Config ({{type}})',
          |||
            sum by (type) (
              pilot_total_xds_rejects{}
            )
          |||
        ),
        self.query(
          'Internal Errors',
          |||
            pilot_total_xds_internal_errors{}
          |||
        ),
        self.query(
          'Push Context Errors',
          |||
            pilot_xds_push_context_errors{}
          |||
        ),
      ],

      xdsConnections: [
        self.query(
          'Connections (client reported)',
          |||
            sum(envoy_cluster_upstream_cx_active{cluster_name="xds-grpc"})
          |||
        ),
        self.query(
          'Connections (server reported)',
          |||
            sum(pilot_xds{})
          |||
        ),
      ],

      pushTime:
        self.query(
          '{{le}}',
          |||
            sum(rate(pilot_xds_push_time_bucket{}[1m])) by (le)
          |||
        ) + q.withFormat('heatmap'),

      pushSize:
        self.query(
          '{{le}}',
          |||
            sum(rate(pilot_xds_config_size_bytes_bucket{}[1m])) by (le)
          |||
        ) + q.withFormat('heatmap'),

      pilotEvents: [
        self.query(
          '{{event}} {{type}}',
          |||
            sum by (type, event) (
              rate(
                pilot_k8s_reg_events{}
              [$__rate_interval])
            )
          |||
        ),
        self.query(
          '{{event}} {{type}}',
          |||
            sum by (type, event) (
              rate(
                pilot_k8s_cfg_events{}
              [$__rate_interval])
            )
          |||
        ),
        self.query(
          'Push {{type}}',
          |||
            sum by (type) (
              rate(
                pilot_push_triggers{}
              [$__rate_interval])
            )
          |||
        ),
      ],

      validateWebhook: [
        self.query(
          'Success',
          |||
            sum(
              rate(
                galley_validation_passed{}
              [$__rate_interval])
            )
          |||
        ),
        self.query(
          'Failure',
          |||
            sum(
              rate(
                galley_validation_passed{}
              [$__rate_interval])
            )
          |||
        ),
      ],

      injectionWebhook: [
        self.query(
          'Success',
          |||
            sum(
              rate(
                sidecar_injection_success_total{}
              [$__rate_interval])
            )
          |||
        ),
        self.query(
          'Failure',
          |||
            sum(
              rate(
                sidecar_injection_failure_total{}
              [$__rate_interval])
            )
          |||
        ),
      ],


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
    },
}
