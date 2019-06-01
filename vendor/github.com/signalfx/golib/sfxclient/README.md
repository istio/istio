# sfxclient

    import "github.com/signalfx/golib/sfxclient"

Package signalfx creates convenient go functions and wrappers to send metrics to
SignalFx.

The core of the library is HTTPSink which allows users to send metrics
to SignalFx ad-hoc. A Scheduler is built on top of this to facility easy
management of metrics for multiple SignalFx reporters at once in more complex
libraries.


### HTTPSink

The simplest way to send metrics to SignalFx is with HTTPSink. The only
struct parameter that needs to be configured is AuthToken. To make it easier to
create common Datapoint objects, wrappers exist for Gauge and Cumulative. An
example of sending a hello world metric would look like this:

    func SendHelloWorld() {
        client := NewHTTPSink()
        client.AuthToken = "ABCDXYZ"
        ctx := context.Background()
        client.AddDatapoints(ctx, []*datapoint.Datapoint{
            GaugeF("hello.world", nil, 1.0),
        })
        dims = make(map[string]string)
        client.AddEvents(ctx, []*event.Event{
            event.New("hello.world", event.USERDEFINED, dims, time.Time{}),
        })
    }


### Scheduler

To facilitate periodic sending of datapoints to SignalFx, a Scheduler
abstraction exists. You can use this to report custom metrics to SignalFx at
some periodic interval.

    type CustomApplication struct {
        queue chan int64
    }
    func (c *CustomApplication) Datapoints() []*datapoint.Datapoint {
        return []*datapoint.Datapoint {
          sfxclient.Gauge("queue.size", nil, len(queue)),
        }
    }
    func main() {
        scheduler := sfxclient.NewScheduler()
        scheduler.Sink.(*HTTPSink).AuthToken = "ABCD-XYZ"
        app := &CustomApplication{}
        scheduler.AddCallback(app)
        go scheduler.Schedule(context.Background())
    }


RollingBucket and CumulativeBucket

Because counting things and calculating percentiles like p99 or median are
common operations, RollingBucket and CumulativeBucket exist to make this easier.
They implement the Collector interface which allows users to add them to an
existing Scheduler.

### Index

* [Constants](#constants)
* [Variables](#variables)
* [func  Cumulative](#func--cumulative)
* [func  CumulativeF](#func--cumulativef)
* [func  CumulativeP](#func--cumulativep)
* [func  Gauge](#func--gauge)
* [func  GaugeF](#func--gaugef)
* [type Collector](#type-collector)
    + [func  NewMultiCollector](#func--newmulticollector)
* [type CumulativeBucket](#type-cumulativebucket)
    + [func (*CumulativeBucket) Add](#func-cumulativebucket-add)
    + [func (*CumulativeBucket) Datapoints](#func-cumulativebucket-datapoints)
    + [func (*CumulativeBucket) MultiAdd](#func-cumulativebucket-multiadd)
* [type HTTPSink](#type-httpsink)
    + [func  NewHTTPSink](#func--newhttpsink)
    + [func (*HTTPSink) AddDatapoints](#func-httpsink-adddatapoints)
    + [func (*HTTPSink) AddEvents](#func-httpsink-addevents)
* [type HashableCollector](#type-hashablecollector)
    + [func  CollectorFunc](#func--collectorfunc)
    + [func (*HashableCollector) Datapoints](#func-hashablecollector-datapoints)
* [type MultiCollector](#type-multicollector)
    + [func (MultiCollector) Datapoints](#func-multicollector-datapoints)
* [type Result](#type-result)
    + [func (*Result) Add](#func-result-add)
* [type RollingBucket](#type-rollingbucket)
    + [func  NewRollingBucket](#func--newrollingbucket)
    + [func (*RollingBucket) Add](#func-rollingbucket-add)
    + [func (*RollingBucket) AddAt](#func-rollingbucket-addat)
    + [func (*RollingBucket) Datapoints](#func-rollingbucket-datapoints)
* [type Scheduler](#type-scheduler)
    + [func  NewScheduler](#func--newscheduler)
    + [func (*Scheduler) AddCallback](#func-scheduler-addcallback)
    + [func (*Scheduler) AddGroupedCallback](#func-scheduler-addgroupedcallback)
    + [func (*Scheduler) DefaultDimensions](#func-scheduler-defaultdimensions)
    + [func (*Scheduler) GroupedDefaultDimensions](#func-scheduler-groupeddefaultdimensions)
    + [func (*Scheduler) RemoveCallback](#func-scheduler-removecallback)
    + [func (*Scheduler) RemoveGroupedCallback](#func-scheduler-removegroupedcallback)
    + [func (*Scheduler) ReportOnce](#func-scheduler-reportonce)
    + [func (*Scheduler) ReportingDelay](#func-scheduler-reportingdelay)
    + [func (*Scheduler) Schedule](#func-scheduler-schedule)
    + [func (*Scheduler) Var](#func-scheduler-var)
* [type Sink](#type-sink)
* [type WithDimensions](#type-withdimensions)
    + [func (*WithDimensions) Datapoints](#func-withdimensions-datapoints)


#### Constants
```go
const ClientVersion = "1.0"
```
ClientVersion is the version of this library and is embedded into the user agent

```go
const DefaultReportingDelay = time.Second * 20
```
DefaultReportingDelay is the default interval Scheduler users to report metrics
to SignalFx

```go
const DefaultTimeout = time.Second * 5
```
DefaultTimeout is the default time to fail signalfx datapoint requests if they
don't succeed

```go
const IngestEndpointV2 = "https://ingest.signalfx.com/v2/datapoint"
```
IngestEndpointV2 is the v2 version of the signalfx ingest endpoint

```go
const TokenHeaderName = "X-Sf-Token"
```
TokenHeaderName is the header key for the auth token in the HTTP request

#### Variables

```go
var DefaultBucketWidth = time.Second * 20
```
DefaultBucketWidth is the default width that a RollingBucket should flush
histogram values

```go
var DefaultErrorHandler = func(err error) error {
	log.DefaultLogger.Log(log.Err, err, "Unable to handle error")
	return nil
}
```
DefaultErrorHandler is the default way to handle errors by a scheduler. It
simply prints them to stdout

```go
var DefaultHistogramSize = 80
```
DefaultHistogramSize is the default number of windows RollingBucket uses for
created histograms

```go
var DefaultMaxBufferSize = 100
```
DefaultMaxBufferSize is the default number of past bucket Quantile values
RollingBucket saves until a Datapoints() call

```go
var DefaultQuantiles = []float64{.25, .5, .9, .99}
```
DefaultQuantiles are the default set of percentiles RollingBucket should collect

```go
var DefaultUserAgent = fmt.Sprintf("golib-sfxclient/%s (gover %s)", ClientVersion, runtime.Version())
```
DefaultUserAgent is the UserAgent string sent to signalfx

#### func  [Cumulative](#cumulative)

```go
func Cumulative(metricName string, dimensions map[string]string, val int64) *datapoint.Datapoint
```
Cumulative creates a SignalFx cumulative counter for integer values.

#### func  [CumulativeF](#cumulativef)

```go
func CumulativeF(metricName string, dimensions map[string]string, val float64) *datapoint.Datapoint
```
CumulativeF creates a SignalFx cumulative counter for float values.

#### func  [CumulativeP](#cumulativep)

```go
func CumulativeP(metricName string, dimensions map[string]string, val *int64) *datapoint.Datapoint
```
CumulativeP creates a SignalFx cumulative counter for integer values from a
pointer that is loaded atomically.

#### func  [Gauge](#gauge)

```go
func Gauge(metricName string, dimensions map[string]string, val int64) *datapoint.Datapoint
```
Gauge creates a SignalFx gauge for integer values.

#### func  [GaugeF](#gaugef)

```go
func GaugeF(metricName string, dimensions map[string]string, val float64) *datapoint.Datapoint
```
GaugeF creates a SignalFx gauge for floating point values.

#### type [Collector](#collector)

```go
type Collector interface {
	Datapoints() []*datapoint.Datapoint
}
```

Collector is anything Scheduler can track that emits points

```go
var GoMetricsSource Collector = &goMetrics{}
```
GoMetricsSource is a singleton Collector that collects basic go system stats. It
currently collects from runtime.ReadMemStats and adds a few extra metrics like
uptime of the process and other runtime package functions.

#### func  [NewMultiCollector](#newmulticollector)

```go
func NewMultiCollector(collectors ...Collector) Collector
```
NewMultiCollector returns a collector that is the aggregate of every given
collector. It can be used to turn multiple collectors into a single collector.

#### type [CumulativeBucket](#cumulativebucket)

```go
type CumulativeBucket struct {
	MetricName string
	Dimensions map[string]string
}
```

A CumulativeBucket tracks groups of values, reporting the count/sum/sum of
squares as a cumulative counter.

#### func (*CumulativeBucket) [Add](#add)

```go
func (b *CumulativeBucket) Add(val int64)
```
Add an item to the bucket, later reporting the result in the next report cycle.

#### func (*CumulativeBucket) [Datapoints](#datapoints)

```go
func (b *CumulativeBucket) Datapoints() []*datapoint.Datapoint
```
Datapoints returns the count/sum/sumsquare datapoints, or nil if there is no set
metric name

#### func (*CumulativeBucket) [MultiAdd](#multiadd)

```go
func (b *CumulativeBucket) MultiAdd(res *Result)
```
MultiAdd many items into the bucket at once using a Result. This can be more
efficient as it involves only a constant number of atomic operations.

#### type [HTTPSink](#httpsink)

```go
type HTTPSink struct {
	AuthToken          string
	UserAgent          string
	DatapointEndpoint  string
    EventEndpoint      string
	Client             http.Client
}
```

HTTPSink will accept signalfx datapoints and forward them to SignalFx
via HTTP.

#### func  [NewHTTPSink](#newhttpsink)

```go
func NewHTTPSink() *HTTPSink
```
NewHTTPSink creates a default NewHTTPSink using package level
constants as defaults, including an empty auth token. If sending directly to
SiganlFx, you will be required to explicitly set the AuthToken

#### func (*HTTPSink) [AddDatapoints](#adddatapoints)

```go
func (h *HTTPSink) AddDatapoints(ctx context.Context, points []*datapoint.Datapoint) (err error)
```
AddDatapoints forwards the datapoints to SignalFx.

#### func (*HTTPSink) [AddEvents](#addevents)

```go
func (h *HTTPSink) AddEvents(ctx context.Context, points []*event.Event) (err error)
```
AddEvents forwards the events to SignalFx.

#### type [HashableCollector](#hashablecollector)

```go
type HashableCollector struct {
	Callback func() []*datapoint.Datapoint
}
```

HashableCollector is a Collector function that can be inserted into a hashmap.
You can use it to wrap a functional callback and insert it into a Scheduler.

#### func  [CollectorFunc](#collectorfunc)

```go
func CollectorFunc(callback func() []*datapoint.Datapoint) *HashableCollector
```
CollectorFunc wraps a function to make it a Collector.

#### func (*HashableCollector) [Datapoints](#datapoints)

```go
func (h *HashableCollector) Datapoints() []*datapoint.Datapoint
```
Datapoints calls the wrapped function.

#### type [MultiCollector](#multicollector)

```go
type MultiCollector []Collector
```

MultiCollector acts like a datapoint collector over multiple collectors.

#### func (MultiCollector) [Datapoints](#datapoints)

```go
func (m MultiCollector) Datapoints() []*datapoint.Datapoint
```
Datapoints returns the datapoints from every collector.

#### type [Result](#result)

```go
type Result struct {
	Count        int64
	Sum          int64
	SumOfSquares float64
}
```

Result is a cumulated result of items that can be added to a CumulativeBucket at
once

#### func (*Result) [Add](#add)

```go
func (r *Result) Add(val int64)
```
Add a single number to the bucket. This does not use atomic operations and is
not thread safe, but adding a finished Result into a CumulativeBucket is thread
safe.

#### type [RollingBucket](#rollingbucket)

```go
type RollingBucket struct {
	// MetricName is the metric name used when the RollingBucket is reported to SignalFx
	MetricName string
	// Dimensions are the dimensions used when the RollingBucket is reported to SignalFx
	Dimensions map[string]string
	// Quantiles are an array of values [0 - 1.0] that are the histogram quantiles reported to
	// SignalFx during a Datapoints() call.  For example, [.5] would only report the median.
	Quantiles []float64
	// BucketWidth is how long in time a bucket accumulates values before a flush is forced
	BucketWidth time.Duration
	// Hist is an efficient tracker of numeric values for a histogram
	Hist *gohistogram.NumericHistogram
	// MaxFlushBufferSize is the maximum size of a window to keep for the RollingBucket before
	// quantiles are dropped.  It is ideally close to len(quantiles) * 3 + 15
	MaxFlushBufferSize int
	// Timer is used to track time.Now() during default value add calls
	Timer timekeeper.TimeKeeper
}
```

RollingBucket keeps histogram style metrics over a BucketWidth window of time.
It allows users to collect and report percentile metrics like like median or
p99, as well as min/max/sum/count and sum of square from a set of points.

#### func  [NewRollingBucket](#newrollingbucket)

```go
func NewRollingBucket(metricName string, dimensions map[string]string) *RollingBucket
```
NewRollingBucket creates a new RollingBucket using default values for Quantiles,
BucketWidth, and the histogram tracker.

#### func (*RollingBucket) [Add](#add)

```go
func (r *RollingBucket) Add(v float64)
```
Add a value to the rolling bucket histogram. If the current time is already
calculated, it may be more efficient to call AddAt in order to save another
time.Time() call.

#### func (*RollingBucket) [AddAt](#addat)

```go
func (r *RollingBucket) AddAt(v float64, t time.Time)
```
AddAt is like Add but also takes a time to pretend the value comes at.

#### func (*RollingBucket) [Datapoints](#datapoints)

```go
func (r *RollingBucket) Datapoints() []*datapoint.Datapoint
```
Datapoints returns basic bucket stats every time and will only the first time
called for each window return that window's points. For efficiency sake,
Datapoints() will only return histogram window values once. Because of this, it
is suggested to always forward datapoints returned by this call to SignalFx.

#### type [Scheduler](#scheduler)

```go
type Scheduler struct {
	Sink             Sink
	Timer            timekeeper.TimeKeeper
	SendZeroTime     bool
	ErrorHandler     func(error) error
	ReportingDelayNs int64
}
```

A Scheduler reports metrics to SignalFx at some timely manner.

#### func  [NewScheduler](#newscheduler)

```go
func NewScheduler() *Scheduler
```
NewScheduler creates a default SignalFx scheduler that can report metrics to
SignalFx at some interval.

#### func (*Scheduler) [AddCallback](#addcallback)

```go
func (s *Scheduler) AddCallback(db Collector)
```
AddCallback adds a collector to the default group.

#### func (*Scheduler) [AddGroupedCallback](#addgroupedcallback)

```go
func (s *Scheduler) AddGroupedCallback(group string, db Collector)
```
AddGroupedCallback adds a collector to a specific group.

#### func (*Scheduler) [DefaultDimensions](#defaultdimensions)

```go
func (s *Scheduler) DefaultDimensions(dims map[string]string)
```
DefaultDimensions adds a dimension map that are appended to all metrics in the
default group.

#### func (*Scheduler) [GroupedDefaultDimensions](#groupeddefaultdimensions)

```go
func (s *Scheduler) GroupedDefaultDimensions(group string, dims map[string]string)
```
GroupedDefaultDimensions adds default dimensions to a specific group.

#### func (*Scheduler) [RemoveCallback](#removecallback)

```go
func (s *Scheduler) RemoveCallback(db Collector)
```
RemoveCallback removes a collector from the default group.

#### func (*Scheduler) [RemoveGroupedCallback](#removegroupedcallback)

```go
func (s *Scheduler) RemoveGroupedCallback(group string, db Collector)
```
RemoveGroupedCallback removes a collector from a specific group.

#### func (*Scheduler) [ReportOnce](#reportonce)

```go
func (s *Scheduler) ReportOnce(ctx context.Context) error
```
ReportOnce will report any metrics saved in this reporter to SignalFx

#### func (*Scheduler) [ReportingDelay](#reportingdelay)

```go
func (s *Scheduler) ReportingDelay(delay time.Duration)
```
ReportingDelay sets the interval metrics are reported to SignalFx.

#### func (*Scheduler) [Schedule](#schedule)

```go
func (s *Scheduler) Schedule(ctx context.Context) error
```
Schedule will run until either the ErrorHandler returns an error or the context
is canceled. This is intended to be run inside a goroutine.

#### func (*Scheduler) [Var](#var)

```go
func (s *Scheduler) Var() expvar.Var
```
Var returns an expvar variable that prints the values of the previously reported
datapoints.

#### type [Sink](#sink)

```go
type Sink interface {
	AddDatapoints(ctx context.Context, points []*datapoint.Datapoint) (err error)
}
```

Sink is anything that can receive points collected by a Scheduler. This can be
useful for stubbing out your collector to test the points that will be sent to
SignalFx.

#### type [WithDimensions](#withdimensions)

```go
type WithDimensions struct {
	Dimensions map[string]string
	Collector  Collector
}
```

WithDimensions adds dimensions on top of the datapoints of a collector. This can
be used to take an existing Collector and include extra dimensions.

#### func (*WithDimensions) [Datapoints](#datapoints)

```go
func (w *WithDimensions) Datapoints() []*datapoint.Datapoint
```
Datapoints calls datapoints and adds on Dimensions
