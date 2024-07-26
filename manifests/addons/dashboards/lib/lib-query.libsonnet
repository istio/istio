{
  sum(query, by=[]):
    if std.length(by) == 0 then
      'sum (%s)' % [query]
    else
      'sum by (%s) (%s)' % [std.join(',', by), query],
  irate(query):
    'irate(%s[$__rate_interval])' % query,
  rate(query):
    'rate(%s[$__rate_interval])' % query,
  round(query, by=0.01):
    'round(%s, %s)' % [query, by],
  quantile(quantile, query):
    'histogram_quantile(%s, %s)' %[quantile, query],
  labels(metric_name, labels):
    // One can nest locals.
    // Every local ends with a semi-colon.
    local prom_labels = std.join(',',
      std.map(
        function(k)
          if std.startsWith(labels[k], "!~") then
            '%s!~"%s"'%[ k, std.lstripChars(labels[k], '!~')]
          else if std.startsWith(labels[k], "~") then
            '%s=~"%s"'%[ k, std.lstripChars(labels[k], '~')]
          else if std.startsWith(labels[k], "!") then
            '%s!="%s"'%[ k, std.lstripChars(labels[k], '!')]
          else 
            '%s="%s"' %[k, labels[k]]
          ,
        std.objectFields(labels)
      ));
      "%s{%s}"%[metric_name, prom_labels],
}
