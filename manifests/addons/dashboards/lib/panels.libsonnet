local g = import 'g.libsonnet';


local overrideSeries = function(series, override)
  g.panel.timeSeries.fieldOverride.byName.new(series)
  + g.panel.timeSeries.fieldOverride.byName.withProperty(
    'displayName',
    override
  );

{
  timeSeries: {
    local timeSeries = g.panel.timeSeries,
    local stat = g.panel.stat,
    local fieldOverride = g.panel.timeSeries.fieldOverride,
    local custom = timeSeries.fieldConfig.defaults.custom,
    local options = timeSeries.options,

    base(title, targets, desc=''):
      timeSeries.new(title)
      + timeSeries.queryOptions.withTargets(targets)
      + timeSeries.queryOptions.withInterval('5s')
      + options.legend.withDisplayMode('table')
      + options.legend.withCalcs([
        'last',
        'max',
      ])
      + custom.withFillOpacity(10)
      + custom.withShowPoints('never')
      + custom.withGradientMode('hue')
      + if std.length(desc) > 0 then
        timeSeries.panelOptions.withDescription(desc)
      else {},

    simple(title, targets, desc=''):
      self.base(title, targets, desc)
      + options.legend.withCalcs([])
      + options.legend.withDisplayMode('list'),

    short(title, targets, desc=''):
      self.base(title, targets, desc)
      + timeSeries.standardOptions.withUnit('short')
      + timeSeries.standardOptions.withDecimals(0),

    seconds(title, targets, desc=''):
      self.base(title, targets, desc)
      + timeSeries.standardOptions.withUnit('s'),

    connections(title, targets, desc=''):
      self.base(title, targets, desc)
      + timeSeries.standardOptions.withUnit('cps'),

    dns(title, targets, desc=''):
      self.base(title, targets, desc)
      + timeSeries.standardOptions.withUnit('qps'),

    bytes(title, targets, desc=''):
      self.base(title, targets, desc)
      + timeSeries.standardOptions.withUnit('bytes'),

    bytesRate(title, targets, desc=''):
      self.base(title, targets, desc)
      + timeSeries.standardOptions.withUnit('Bps'),

    stat(title, targets, desc=''):
      stat.new(title)
      + stat.queryOptions.withTargets(targets)
      + stat.queryOptions.withInterval('5s')
      + options.legend.withDisplayMode('table')
      + options.legend.withCalcs([
        'last',
        'max',
      ])
      + stat.standardOptions.color.withFixedColor('blue')
      + stat.standardOptions.color.withMode('fixed')
      + custom.withFillOpacity(10)
      + custom.withShowPoints('never')
      + custom.withGradientMode('hue')
      + if std.length(desc) > 0 then
        stat.panelOptions.withDescription(desc)
      else {},

    statRps(title, targets, desc=''):
      self.stat(title, targets, desc)
      + timeSeries.standardOptions.withUnit('reqps'),

    statPercent(title, targets, desc=''):
      self.stat(title, targets, desc)
      + timeSeries.standardOptions.withUnit('percentunit'),

    allocations(title, targets, desc=''):
      self.base(title, targets, desc)
      + timeSeries.standardOptions.withUnit('Bps')
      + timeSeries.standardOptions.withOverrides([
        fieldOverride.byQuery.new('B')
        + fieldOverride.byQuery.withProperty('custom.axisPlacement', 'right')
        + fieldOverride.byQuery.withProperty('unit', 'c/s'),
      ])
    ,

    durationQuantile(title, targets, desc=''):
      self.base(title, targets, desc)
      + timeSeries.standardOptions.withUnit('s')
      + custom.withDrawStyle('bars')
      + timeSeries.standardOptions.withOverrides([
        fieldOverride.byRegexp.new('/mean/i')
        + fieldOverride.byRegexp.withProperty(
          'custom.fillOpacity',
          0
        )
        + fieldOverride.byRegexp.withProperty(
          'custom.lineStyle',
          {
            dash: [8, 10],
            fill: 'dash',
          }
        ),
      ]),


    bars(title, targets, desc=''):
      self.base(title, targets, desc)
      + options.legend.withCalcs([])
      + options.legend.withDisplayMode('list')
      + custom.withDrawStyle('bars')
      + custom.withStacking({ mode: 'normal' })
      + timeSeries.queryOptions.withInterval('15s')
      + timeSeries.standardOptions.withUnit('ops')
      + custom.withFillOpacity(100)
      + custom.withShowPoints('never')
      + custom.withGradientMode('none'),

    xdsPushes(title, targets, desc=''):
      self.bars(title, targets, desc='')
      + timeSeries.standardOptions.withOverrides([
        overrideSeries('cds', 'Clusters'),
        overrideSeries('eds', 'Endpoints'),
        overrideSeries('lds', 'Listeners'),
        overrideSeries('rds', 'Routes'),
        overrideSeries('nds', 'DNS Tables'),
        overrideSeries('istio.io/debug', 'Debug'),
        overrideSeries('istio.io/debug/syncz', 'Debug'),
        overrideSeries('wads', 'Authorization'),
        overrideSeries('wds', 'Workloads'),
        overrideSeries('type.googleapis.com/istio.security.Authorization', 'Authorizations'),
        overrideSeries('type.googleapis.com/istio.workload.Address', 'Addresses'),
      ]),
  },

  tables: {
    local table = g.panel.table,
    local override = table.fieldOverride,
    base(title, targets, desc=''):
      table.new(title)
      + table.queryOptions.withTargets(targets)
      + table.queryOptions.withInterval('5s')
      + if std.length(desc) > 0 then
        table.panelOptions.withDescription(desc)
      else {},

    requests(title, targets, desc=''):
      self.base(title, targets, desc)
      + table.queryOptions.withTransformations({ id: 'merge' })
      + table.standardOptions.withOverrides([
        // Query name/unit
        override.byName.new('Value #requests')
        + override.byName.withProperty('displayName', 'Requests')
        + override.byName.withProperty('decimals', 2)
        + override.byName.withProperty('unit', 'reqps'),
        override.byName.new('Value #p50')
        + override.byName.withProperty('displayName', 'P50 Latency')
        + override.byName.withProperty('decimals', 2)
        + override.byName.withProperty('unit', 'ms'),
        override.byName.new('Value #p90')
        + override.byName.withProperty('displayName', 'P90 Latency')
        + override.byName.withProperty('decimals', 2)
        + override.byName.withProperty('unit', 'ms'),
        override.byName.new('Value #p99')
        + override.byName.withProperty('displayName', 'P99 Latency')
        + override.byName.withProperty('decimals', 2)
        + override.byName.withProperty('unit', 'ms'),
        override.byName.new('Value #success')
        + override.byName.withProperty('displayName', 'Success Rate')
        + override.byName.withProperty('decimals', 2)
        + override.byName.withProperty('unit', 'percentunit')
        + override.byName.withProperty('custom.cellOptions', { type: 'color-background' })
        + override.byName.withProperty('thresholds', { mode: 'absolute', steps: [
          {color: "red", value: null},
          {color: "yellow", value: "0.95"},
          {color: "green", value: 1},
        ] }),

        // Key
        override.byName.new('destination_workload_var')
        + override.byName.withProperty('displayName', 'Workload'),
        override.byName.new('destination_service')
        + override.byName.withProperty('displayName', 'Service')
        + override.byName.withProperty('custom.minWidth', 400),
        override.byName.new('destination_workload_namespace')
        + override.byName.withProperty('custom.hidden', true),
        override.byName.new('destination_workload')
        + override.byName.withProperty('custom.hidden', true),
        override.byName.new('Time')
        + override.byName.withProperty('custom.hidden', true),
      ]),

    tcpRequests(title, targets, desc=''):
      self.base(title, targets, desc)
      + table.queryOptions.withTransformations({ id: 'merge' })
      + table.standardOptions.withOverrides([
        // Query name/unit
        override.byName.new('Value #recv')
        + override.byName.withProperty('displayName', 'Bytes Received')
        + override.byName.withProperty('decimals', 2)
        + override.byName.withProperty('unit', 'bps'),
        override.byName.new('Value #sent')
        + override.byName.withProperty('displayName', 'Bytes Sent')
        + override.byName.withProperty('decimals', 2)
        + override.byName.withProperty('unit', 'bps'),

        // Key
        override.byName.new('destination_workload_var')
        + override.byName.withProperty('displayName', 'Workload'),
        override.byName.new('destination_service')
        + override.byName.withProperty('displayName', 'Service')
        + override.byName.withProperty('custom.minWidth', 400),
        override.byName.new('destination_workload_namespace')
        + override.byName.withProperty('custom.hidden', true),
        override.byName.new('destination_workload')
        + override.byName.withProperty('custom.hidden', true),
        override.byName.new('Time')
        + override.byName.withProperty('custom.hidden', true),
      ]),
  },

  heatmap: {
    local heatmap = g.panel.heatmap,
    local options = heatmap.options,

    base(title, targets, desc=''):
      heatmap.new(title)
      + heatmap.queryOptions.withTargets(targets)
      + heatmap.queryOptions.withInterval('1m')
      + options.calculation.xBuckets.withMode('size')
      + options.calculation.xBuckets.withValue('1min')
      + options.withCellGap(0)
      + options.color.withMode('scheme')
      + options.color.withScheme('Spectral')
      + options.color.withSteps(128)
      + options.yAxis.withDecimals(0)
      + options.yAxis.withUnit('s')
      + if std.length(desc) > 0 then
        heatmap.panelOptions.withDescription(desc)
      else {},

    bytes(title, targets, desc=''):
      self.base(title, targets, desc)
      + options.yAxis.withUnit('bytes'),
  },
}
