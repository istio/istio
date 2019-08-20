## Circonus gometrics options

### Example defaults
```go
package main

import (
    "fmt"
    "io/ioutil"
    "log"
    "os"
    "path"

    cgm "github.com/circonus-labs/circonus-gometrics"
)

func main() {
    cfg := &cgm.Config{}

    // Defaults

    // General
    cfg.Debug = false
    cfg.Log = log.New(ioutil.Discard, "", log.LstdFlags)
    cfg.Interval = "10s"
    cfg.ResetCounters = "true"
    cfg.ResetGauges = "true"
    cfg.ResetHistograms = "true"
    cfg.ResetText = "true"

    // API
    cfg.CheckManager.API.TokenKey = ""
    cfg.CheckManager.API.TokenApp = "circonus-gometrics"
    cfg.CheckManager.API.TokenURL = "https://api.circonus.com/v2"
    cfg.CheckManager.API.CACert = nil
    cfg.CheckManager.API.TLSConfig = nil

    // Check
    _, an := path.Split(os.Args[0])
    hn, _ := os.Hostname()
    cfg.CheckManager.Check.ID = ""
    cfg.CheckManager.Check.SubmissionURL = ""
    cfg.CheckManager.Check.InstanceID = fmt.Sprintf("%s:%s", hn, an)
    cfg.CheckManager.Check.TargetHost = cfg.CheckManager.Check.InstanceID
    cfg.CheckManager.Check.DisplayName = cfg.CheckManager.Check.InstanceID
    cfg.CheckManager.Check.SearchTag = fmt.Sprintf("service:%s", an)
    cfg.CheckManager.Check.Tags = ""
    cfg.CheckManager.Check.Secret = "" // randomly generated sha256 hash
    cfg.CheckManager.Check.MaxURLAge = "5m"
    cfg.CheckManager.Check.ForceMetricActivation = "false"

    // Broker
    cfg.CheckManager.Broker.ID = ""
    cfg.CheckManager.Broker.SelectTag = ""
    cfg.CheckManager.Broker.MaxResponseTime = "500ms"
    cfg.CheckManager.Broker.TLSConfig = nil

    // create a new cgm instance and start sending metrics...
    // see the complete example in the main README.    
}
```

## Options
| Option | Default | Description |
| ------ | ------- | ----------- |
| General ||
| `cfg.Log` | none | log.Logger instance to send logging messages. Default is to discard messages. If Debug is turned on and no instance is specified, messages will go to stderr. |
| `cfg.Debug` | false | Turn on debugging messages. |
| `cfg.Interval` | "10s" | Interval at which metrics are flushed and sent to Circonus. Set to "0s" to disable automatic flush (note, if disabled, `cgm.Flush()` must be called manually to send metrics to Circonus).|
| `cfg.ResetCounters` | "true" | Reset counter metrics after each submission. Change to "false" to retain (and continue submitting) the last value.|
| `cfg.ResetGauges` | "true" | Reset gauge metrics after each submission. Change to "false" to retain (and continue submitting) the last value.|
| `cfg.ResetHistograms` | "true" | Reset histogram metrics after each submission. Change to "false" to retain (and continue submitting) the last value.|
| `cfg.ResetText` | "true" | Reset text metrics after each submission. Change to "false" to retain (and continue submitting) the last value.|
|API||
| `cfg.CheckManager.API.TokenKey` | "" | [Circonus API Token key](https://login.circonus.com/user/tokens) |
| `cfg.CheckManager.API.TokenApp` | "circonus-gometrics" | App associated with API token |
| `cfg.CheckManager.API.URL` | "https://api.circonus.com/v2" | Circonus API URL |
| `cfg.CheckManager.API.TLSConfig` | nil | Custom tls.Config to use when communicating with Circonus API |
| `cfg.CheckManager.API.CACert` | nil | DEPRECATED - use TLSConfig ~~[*x509.CertPool](https://golang.org/pkg/crypto/x509/#CertPool) with CA Cert to validate API endpoint using internal CA or self-signed certificates~~ |
|Check||
| `cfg.CheckManager.Check.ID` | "" | Check ID of previously created check. (*Note: **check id** not **check bundle id**.*) |
| `cfg.CheckManager.Check.SubmissionURL` | "" | Submission URL of previously created check. Metrics can also be sent to a local [circonus-agent](https://github.com/circonus-labs/circonus-agent) by using the agent's URL (e.g. `http://127.0.0.1:2609/write/appid` where `appid` is a unique identifier for the application which will prefix all metrics. Additionally, the circonus-agent can optionally listen for requests to `/write` on a unix socket - to leverage this feature, use a URL such as `http+unix:///path/to/socket_file/write/appid`). |
| `cfg.CheckManager.Check.InstanceID` | hostname:program name | An identifier for the 'group of metrics emitted by this process or service'. |
| `cfg.CheckManager.Check.TargetHost` | InstanceID | Explicit setting of `check.target`. |
| `cfg.CheckManager.Check.DisplayName` | InstanceID | Custom `check.display_name`. Shows in UI check list. |
| `cfg.CheckManager.Check.SearchTag` | service:program name | Specific tag used to search for an existing check when neither SubmissionURL nor ID are provided. |
| `cfg.CheckManager.Check.Tags` | "" | List (comma separated) of tags to add to check when it is being created. The SearchTag will be added to the list. |
| `cfg.CheckManager.Check.Secret` | random generated | A secret to use for when creating an httptrap check. |
| `cfg.CheckManager.Check.MaxURLAge` | "5m" | Maximum amount of time to retry a [failing] submission URL before refreshing it. |
| `cfg.CheckManager.Check.ForceMetricActivation` | "false" | If a metric has been disabled via the UI the default behavior is to *not* re-activate the metric; this setting overrides the behavior and will re-activate the metric when it is encountered. |
|Broker||
| `cfg.CheckManager.Broker.ID` | "" | ID of a specific broker to use when creating a check. Default is to use a random enterprise broker or the public Circonus default broker. |
| `cfg.CheckManager.Broker.SelectTag` | "" | Used to select a broker with the same tag(s). If more than one broker has the tag(s), one will be selected randomly from the resulting list. (e.g. could be used to select one from a list of brokers serving a specific colo/region. "dc:sfo", "loc:nyc,dc:nyc01", "zone:us-west") |
| `cfg.CheckManager.Broker.MaxResponseTime` | "500ms" | Maximum amount time to wait for a broker connection test to be considered valid. (if latency is > the broker will be considered invalid and not available for selection.) |
| `cfg.CheckManager.Broker.TLSConfig` | nil | Custom tls.Config to use when communicating with Circonus Broker |

## Notes:

* All options are *strings* with the following exceptions:
   * `cfg.Log` - an instance of [`log.Logger`](https://golang.org/pkg/log/#Logger) or something else (e.g. [logrus](https://github.com/Sirupsen/logrus)) which can be used to satisfy the interface requirements.
   * `cfg.Debug` - a boolean true|false.
* At a minimum, one of either `API.TokenKey` or `Check.SubmissionURL` is **required** for cgm to function.
* Check management can be disabled by providing a `Check.SubmissionURL` without an `API.TokenKey`. Note: the supplied URL needs to be http or the broker needs to be running with a cert which can be verified. Otherwise, the `API.TokenKey` will be required to retrieve the correct CA certificate to validate the broker's cert for the SSL connection.
* A note on `Check.InstanceID`, the instance id is used to consistently identify a check. The display name can be changed in the UI. The hostname may be ephemeral. For metric continuity, the instance id is used to locate existing checks. Since the check.target is never actually used by an httptrap check it is more decorative than functional, a valid FQDN is not required for an httptrap check.target. But, using instance id as the target can pollute the Host list in the UI with host:application specific entries.
* Check identification precedence
   1. Check SubmissionURL
   2. Check ID
   3. Search
      1. Search for an active httptrap check for TargetHost which has the SearchTag
      2. Search for an active httptrap check which has the SearchTag and the InstanceID in the notes field
      3. Create a new check
* Broker selection
   1. If Broker.ID or Broker.SelectTag are not specified, a broker will be selected randomly from the list of brokers available to the API token. Enterprise brokers take precedence. A viable broker is "active", has the "httptrap" module enabled, and responds within Broker.MaxResponseTime.
