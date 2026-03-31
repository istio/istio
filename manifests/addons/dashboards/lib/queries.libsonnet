local g = import './g.libsonnet';
local q = g.query.prometheus;

local query = import './lib-query.libsonnet';
local sum = query.sum;
local rate = query.rate;
local irate = query.irate;
local labels = query.labels;
local round = query.round;
local quantile = query.quantile;

local variables = import './variables.libsonnet';

{
  queries(names):
    local containerLabels = { container: names.container, pod: '~' + names.pod };
    local appLabels = { app: names.app };
    local podLabels = { pod: '~' + names.pod };
    {
      query(legend, query):
        self.rawQuery(query)
        + q.withLegendFormat(legend),

      rawQuery(query):
        q.new(
          '$' + variables.datasource.name,
          std.rstripChars(query, '\n')
        ),

      allIstioBuild:
        self.query(
          '{{component}} ({{tag}})',
          sum('istio_build', by=['component', 'tag'])
        ),

      istioBuild:
        self.query(
          'Version ({{tag}})',
          sum(labels('istio_build', { component: names.component }), by=['tag'])
        ),

      cpuUsage:
        self.query(
          'Container ({{pod}})',
          sum(irate(labels('container_cpu_usage_seconds_total', containerLabels)), by=['pod'])
        ),

      memUsage:
        self.query(
          'Container ({{pod}})',
          sum(labels('container_memory_working_set_bytes', containerLabels), by=['pod'])
        ),

      goMemoryUsage: [
        self.query(
          'Container ({{pod}})',
          sum(labels('container_memory_working_set_bytes', containerLabels), by=['pod'])
        ),
        self.query(
          'Stack ({{pod}})',
          sum(labels('go_memstats_stack_inuse_bytes', appLabels), by=['pod'])
        ),
        self.query(
          'Heap (In Use) ({{pod}})',
          sum(labels('go_memstats_heap_inuse_bytes', appLabels), by=['pod'])
        ),
        self.query(
          'Heap (Allocated) ({{pod}})',
          sum(labels('go_memstats_heap_alloc_bytes', appLabels), by=['pod'])
        ),
      ],

      goAllocations: [
        self.query(
          'Bytes ({{pod}})',
          sum(rate(labels('go_memstats_alloc_bytes_total', appLabels)), by=['pod'])
        ),
        self.query(
          'Objects ({{pod}})',
          sum(rate(labels('go_memstats_mallocs_total', appLabels)), by=['pod'])
        ),
      ],

      goroutines:
        self.query(
          'Goroutines ({{pod}})',
          sum(labels('go_goroutines', appLabels), by=['pod'])
        ),

      connections:
        [
          self.query(
            'Opened ({{pod}})',
            sum(rate(labels('istio_tcp_connections_opened_total', podLabels)), by=['pod'])
          ),
          self.query(
            'Closed ({{pod}})',
            '-' + sum(rate(labels('istio_tcp_connections_closed_total', podLabels)), by=['pod'])
          ),
        ],

      bytes:
        [
          self.query(
            'Sent ({{pod}})',
            sum(rate(labels('istio_tcp_sent_bytes_total', podLabels)), by=['pod'])
          ),
          self.query(
            'Received ({{pod}})',
            sum(rate(labels('istio_tcp_received_bytes_total', podLabels)), by=['pod'])
          ),
        ],

      dns:
        self.query(
          'Request ({{pod}})',
          sum(rate(labels('istio_dns_requests_total', podLabels)), by=['pod'])
        ),

      ztunnelXdsConnections:
        self.query(
          'XDS Connection Terminations ({{pod}})',
          sum(rate(labels('istio_xds_connection_terminations_total', podLabels)), by=['pod'])
        ),

      ztunnelXdsMessages:
        self.query(
          '{{url}}',
          sum(irate(labels('istio_xds_message_total', podLabels)), by=['url'])
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
            sum(rate(pilot_xds_push_time_bucket{}[$__rate_interval])) by (le)
          |||
        ) + q.withFormat('heatmap'),

      pushSize:
        self.query(
          '{{le}}',
          |||
            sum(rate(pilot_xds_config_size_bytes_bucket{}[$__rate_interval])) by (le)
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
          sum(labels('workload_manager_active_proxy_count', podLabels), by=['pod'])
        ),
        self.query(
          'Pending Proxies ({{pod}})',
          sum(labels('workload_manager_pending_proxy_count', podLabels), by=['pod'])
        ),
      ],

      globalRequest: self.rawQuery(
        round(sum(rate(labels('istio_requests_total', { reporter: '~source|waypoint' }))))
      ),

      globalRequestSuccessRate: self.rawQuery(
        sum(rate(labels('istio_requests_total', { reporter: '~source|waypoint', response_code: '!~5..' }))) + ' / ' +
        sum(rate(labels('istio_requests_total', { reporter: '~source|waypoint' })))
      ),

      globalRequest4xx: self.rawQuery(
        round(sum(rate(labels('istio_requests_total', { reporter: '~source|waypoint', response_code: '~4..' })))) + 'or vector(0)'
      ),

      globalRequest5xx: self.rawQuery(
        round(sum(rate(labels('istio_requests_total', { reporter: '~source|waypoint', response_code: '~5..' })))) + 'or vector(0)'
      ),

      local tableLabelJoin = function(query)
        'label_join('
        + query
        + ', "destination_workload_var", ".", "destination_workload", "destination_workload_namespace")',

      httpWorkloads: [
        // Request total
        self.query(
          '{{ destination_workload}}.{{ destination_workload_namespace }}',
          tableLabelJoin(sum(
            rate(labels('istio_requests_total', { reporter: '~source|waypoint' })),
            by=['destination_workload', 'destination_workload_namespace', 'destination_service']
          ))
        ) + q.withFormat('table') + q.withRefId('requests') + q.withInstant(),
        // P50
        self.query(
          '{{ destination_workload}}.{{ destination_workload_namespace }}',
          tableLabelJoin(
            quantile(
              '0.5',
              sum(
                rate(labels('istio_request_duration_milliseconds_bucket', { reporter: '~source|waypoint' })),
                by=['le', 'destination_workload', 'destination_workload_namespace']
              )
            )
          )
        ) + q.withFormat('table') + q.withRefId('p50') + q.withInstant(),
        // P90
        self.query(
          '{{ destination_workload}}.{{ destination_workload_namespace }}',
          tableLabelJoin(
            quantile(
              '0.9',
              sum(
                rate(labels('istio_request_duration_milliseconds_bucket', { reporter: '~source|waypoint' })),
                by=['le', 'destination_workload', 'destination_workload_namespace']
              )
            )
          )
        ) + q.withFormat('table') + q.withRefId('p90') + q.withInstant(),
        // P99
        self.query(
          '{{ destination_workload}}.{{ destination_workload_namespace }}',
          tableLabelJoin(
            quantile(
              '0.99',
              sum(
                rate(labels('istio_request_duration_milliseconds_bucket', { reporter: '~source|waypoint' })),
                by=['le', 'destination_workload', 'destination_workload_namespace']
              )
            )
          )
        ) + q.withFormat('table') + q.withRefId('p99') + q.withInstant(),
        // Success Rate
        self.query(
          '{{ destination_workload}}.{{ destination_workload_namespace }}',
          tableLabelJoin(
            sum(
              rate(labels('istio_requests_total', { reporter: '~source|waypoint', response_code: '!~5..' })),
              by=['destination_workload', 'destination_workload_namespace']
            )
            + '/' +
            sum(
              rate(labels('istio_requests_total', { reporter: '~source|waypoint' })),
              by=['destination_workload', 'destination_workload_namespace']
            )
          )
        ) + q.withFormat('table') + q.withRefId('success') + q.withInstant(),
      ],

      tcpWorkloads: [
        self.query(
          '{{ destination_workload}}.{{ destination_workload_namespace }}',
          tableLabelJoin(sum(
            rate(labels('istio_tcp_received_bytes_total', { reporter: '~source|waypoint' })),
            by=['destination_workload', 'destination_workload_namespace', 'destination_service']
          ))
        ) + q.withFormat('table') + q.withRefId('recv') + q.withInstant(),
        self.query(
          '{{ destination_workload}}.{{ destination_workload_namespace }}',
          tableLabelJoin(sum(
            rate(labels('istio_tcp_sent_bytes_total', { reporter: '~source|waypoint' })),
            by=['destination_workload', 'destination_workload_namespace', 'destination_service']
          ))
        ) + q.withFormat('table') + q.withRefId('sent') + q.withInstant(),
      ],
    },
}
