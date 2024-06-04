local g = import 'g.libsonnet';

{
  timeSeries: {
    local timeSeries = g.panel.timeSeries,
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
  },


  heatmap: {
    local heatmap = g.panel.heatmap,
    local options = heatmap.options,

    base(title, targets):
      heatmap.new(title)
      + heatmap.queryOptions.withTargets(targets)
      + heatmap.queryOptions.withInterval('1m')
      + options.withCalculate()
      + options.calculation.xBuckets.withMode('size')
      + options.calculation.xBuckets.withValue('1min')
      + options.withCellGap(2)
      + options.color.withMode('scheme')
      + options.color.withScheme('Spectral')
      + options.color.withSteps(128)
      + options.yAxis.withDecimals(0)
      + options.yAxis.withUnit('s'),
  },
}

// vim: foldmethod=marker foldmarker=local,;
