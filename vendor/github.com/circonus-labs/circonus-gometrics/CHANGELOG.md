# v2.3.1

* fix: incorrect attribute types in graph overlays (docs vs what api actually returns)

# v2.3.0

* fix: graph structures incorrectly represented nesting of overlay sets

# v2.2.7

* add: `search` (`*string`) attribute to graph datapoint
* add: `cluster_ip` (`*string`) attribute to broker details

# v2.2.6

* fix: func signature to match go-retryablehttp update
* upd: dependency go-retryablehttp, lock to v0.5.2 to prevent future breaking patch features

# v2.2.5

* upd: switch from tracking master to versions for retryablehttp and circonusllhist now that both repositories are doing releases

# v2.2.4

* fix: worksheet.graphs is a required attribute. worksheet.smart_queries is an optional attribute.

# v2.2.3

* upd: remove go.{mod,dep} as cgm being v2 causes more issues than it solves at this point. will re-add after `go mod` becomes more common and adding `v2` to all internal import statements won't cause additional issues.

# v2.2.2

* upd: add go.mod and go.sum

# v2.2.1

* fix: if submission url host is 'api.circonus.com' do not use private CA in TLSConfig

# v2.2.0

* fix: do not reset counter|gauge|text funcs after each snapshot (only on explicit call to Reset)
* upd: dashboards - optional widget attributes - which are structs - should be pointers for correct omission in json sent to api
* fix: dashboards - remove `omitempty` from required attributes
* fix: graphs - remove `omitempty` from required attributes
* fix: worksheets - correct attribute name, remove `omitempty` from required attributes
* fix: handle case where a broker has no external host or ip set

# v2.1.2

* upd: breaking change in upstream repo
* upd: upstream deps

# v2.1.1

* dep dependencies
* fix two instances of shadowed variables
* fix several documentation typos
* simplify (gofmt -s)
* remove an inefficient use of regexp.MatchString

# v2.1.0

* Add unix socket capability for SubmissionURL `http+unix://...`
* Add `RecordCountForValue` function to histograms

# v2.0.0

* gauges as `interface{}`
   * change: `GeTestGauge(string) (string,error)` ->  `GeTestGauge(string) (interface{},error)`
   * add: `AddGauge(string, interface{})` to add a delta value to an existing gauge
* prom output candidate
* Add `CHANGELOG.md` to repository
