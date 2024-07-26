local g = import './g.libsonnet';
local q = g.query.prometheus;

local query = import './lib-query.libsonnet';
local sum = query.sum;
local rate = query.rate;
local irate = query.irate;
local labels = query.labels;

local variables = import './variables.libsonnet';

{
  queries(names):
    local containerLabels2 = { container: names.container, pod: '~' + names.pod };
    local containerLabels = 'container="%(container)s", pod=~"%(pod)s"' % names;
    local appLabels = 'app="%(app)s"' % names;
    local appLabels2 = { app: names.app };
    local podLabels = 'pod=~"%(pod)s"' % names;
    local podLabels2 = { pod: '~' + names.pod };
    {
      query(legend, query):
        q.new(
          '$' + variables.datasource.name,
          std.rstripChars(query % { containerLabels: containerLabels, podLabels: podLabels, appLabels: appLabels }, '\n')
        )
        + q.withLegendFormat(legend),

      istioBuild:
        self.query(
          'Version ({{tag}})',
          sum(labels('istio_build', { component: names.component }), by=['tag'])
        ),

      cpuUsage:
        self.query(
          'Container ({{pod}})',
          sum(irate(labels('container_cpu_usage_seconds_total', containerLabels2)), by=['pod'])
        ),

      memUsage:
        self.query(
          'Container ({{pod}})',
          sum(labels('container_memory_working_set_bytes', containerLabels2), by=['pod'])
        ),

      goMemoryUsage: [
        self.query(
          'Container ({{pod}})',
          sum(labels('container_memory_working_set_bytes', containerLabels2), by=['pod'])
        ),
        self.query(
          'Stack ({{pod}})',
          sum(labels('go_memstats_stack_inuse_bytes', appLabels2), by=['pod'])
        ),
        self.query(
          'Heap (In Use) ({{pod}})',
          sum(labels('go_memstats_heap_inuse_bytes', appLabels2), by=['pod'])
        ),
        self.query(
          'Heap (Allocated) ({{pod}})',
          sum(labels('go_memstats_heap_alloc_bytes', appLabels2), by=['pod'])
        ),
      ],

      goAllocations: [
        self.query(
          'Bytes ({{pod}})',
          sum(rate(labels('go_memstats_alloc_bytes_total', appLabels2)), by=['pod'])
        ),
        self.query(
          'Objects ({{pod}})',
          sum(rate(labels('go_memstats_mallocs_total', appLabels2)), by=['pod'])
        ),
      ],

      goroutines:
        self.query(
          'Goroutines ({{pod}})',
          sum(labels('go_goroutines', appLabels2), by=['pod'])
        ),

      connections:
        [
          self.query(
            'Opened ({{pod}})',
            sum(rate(labels('istio_tcp_connections_opened_total', podLabels2)), by=['pod'])
          ),
          self.query(
            'Closed ({{pod}})',
            '-' + sum(rate(labels('istio_tcp_connections_closed_total', podLabels2)), by=['pod'])
          ),
        ],

      bytes:
        [
          self.query(
            'Sent ({{pod}})',
            sum(rate(labels('istio_tcp_sent_bytes_total', podLabels2)), by=['pod'])
          ),
          self.query(
            'Received ({{pod}})',
            sum(rate(labels('istio_tcp_received_bytes_total', podLabels2)), by=['pod'])
          ),
        ],

      dns:
        self.query(
          'Request ({{pod}})',
          sum(rate(labels('istio_dns_requests_total', podLabels2)), by=['pod'])
        ),

      ztunnelXdsConnections:
        self.query(
          'XDS Connection Terminations ({{pod}})',
          sum(rate(labels('istio_xds_connection_terminations_total', podLabels2)), by=['pod'])
        ),

      ztunnelXdsMessages:
        self.query(
          '{{url}}',
          sum(irate(labels('istio_xds_message_total', podLabels2)), by=['url'])
        ),

      xdsPushes:
        self.query(
          '{{type}}',
          sum(irate('pilot_xds_pushes'), by=['type'])
        ),

      xdsErrors: [
        self.query(
          'Rejected Config ({{type}})',
          sum('pilot_total_xds_rejects', by=['type'])
        ),
        self.query(
          'Internal Errors',
          'pilot_total_xds_internal_errors'
        ),
        self.query(
          'Push Context Errors',
          'pilot_xds_push_context_errors'
        ),
      ],

      xdsConnections: [
        self.query(
          'Connections (client reported)',
          'sum(envoy_cluster_upstream_cx_active{cluster_name="xds-grpc"})'
        ),
        self.query(
          'Connections (server reported)',
          sum('pilot_xds')
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
          sum(rate('pilot_k8s_reg_events'), by=['type', 'event'])
        ),
        self.query(
          '{{event}} {{type}}',
          sum(rate('pilot_k8s_cfg_events'), by=['type', 'event'])
        ),
        self.query(
          'Push {{type}}',
          sum(rate('pilot_push_triggers'), by=['type'])
        ),
      ],

      validateWebhook: [
        self.query(
          'Success',
          sum(rate('galley_validation_passed'))
        ),
        self.query(
          'Failure',
          sum(rate('galley_validation_failed'))
        ),
      ],

      injectionWebhook: [
        self.query(
          'Success',
          sum(rate('sidecar_injection_success_total'))
        ),
        self.query(
          'Failure',
          sum(rate('sidecar_injection_failure_total'))
        ),
      ],


      workloadManager: [
        self.query(
          'Active Proxies ({{pod}})',
          sum(labels('workload_manager_active_proxy_count', podLabels2), by=['pod'])
        ),
        self.query(
          'Pending Proxies ({{pod}})',
          sum(labels('workload_manager_pending_proxy_count', podLabels2), by=['pod'])
        ),
      ],
    },
}
