// Copyright 2016 Circonus, Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package checkmgr provides a check management interface to circonus-gometrics
package checkmgr

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/circonus-labs/circonus-gometrics/api"
	"github.com/pkg/errors"
	"github.com/tv42/httpunix"
)

// Check management offers:
//
// Create a check if one cannot be found matching specific criteria
// Manage metrics in the supplied check (enabling new metrics as they are submitted)
//
// To disable check management, leave Config.Api.Token.Key blank
//
// use cases:
// configure without api token - check management disabled
//  - configuration parameters other than Check.SubmissionUrl, Debug and Log are ignored
//  - note: SubmissionUrl is **required** in this case as there is no way to derive w/o api
// configure with api token - check management enabled
//  - all other configuration parameters affect how the trap url is obtained
//    1. provided (Check.SubmissionUrl)
//    2. via check lookup (CheckConfig.Id)
//    3. via a search using CheckConfig.InstanceId + CheckConfig.SearchTag
//    4. a new check is created

const (
	defaultCheckType             = "httptrap"
	defaultTrapMaxURLAge         = "60s"   // 60 seconds
	defaultBrokerMaxResponseTime = "500ms" // 500 milliseconds
	defaultForceMetricActivation = "false"
	statusActive                 = "active"
)

// CheckConfig options for check
type CheckConfig struct {
	// a specific submission url
	SubmissionURL string
	// a specific check id (not check bundle id)
	ID string
	// unique instance id string
	// used to search for a check to use
	// used as check.target when creating a check
	InstanceID string
	// explicitly set check.target (default: instance id)
	TargetHost string
	// a custom display name for the check (as viewed in UI Checks)
	// default: instance id
	DisplayName string
	// unique check searching tag (or tags)
	// used to search for a check to use (combined with instanceid)
	// used as a regular tag when creating a check
	SearchTag string
	// httptrap check secret (for creating a check)
	Secret string
	// additional tags to add to a check (when creating a check)
	// these tags will not be added to an existing check
	Tags string
	// max amount of time to to hold on to a submission url
	// when a given submission fails (due to retries) if the
	// time the url was last updated is > than this, the trap
	// url will be refreshed (e.g. if the broker is changed
	// in the UI) **only relevant when check management is enabled**
	// e.g. 5m, 30m, 1h, etc.
	MaxURLAge string
	// force metric activation - if a metric has been disabled via the UI
	// the default behavior is to *not* re-activate the metric; this setting
	// overrides the behavior and will re-activate the metric when it is
	// encountered. "(true|false)", default "false"
	ForceMetricActivation string
	// Type of check to use (default: httptrap)
	Type string
	// Custom check config fields (default: none)
	CustomConfigFields map[string]string
}

// BrokerConfig options for broker
type BrokerConfig struct {
	// a specific broker id (numeric portion of cid)
	ID string
	// one or more tags used to select 1-n brokers from which to select
	// when creating a new check (e.g. datacenter:abc or loc:dfw,dc:abc)
	SelectTag string
	// for a broker to be considered viable it must respond to a
	// connection attempt within this amount of time e.g. 200ms, 2s, 1m
	MaxResponseTime string
	// TLS configuration to use when communicating within broker
	TLSConfig *tls.Config
}

// Config options
type Config struct {
	Log   *log.Logger
	Debug bool

	// Circonus API config
	API api.Config
	// Check specific configuration options
	Check CheckConfig
	// Broker specific configuration options
	Broker BrokerConfig
}

// CheckTypeType check type
type CheckTypeType string

// CheckInstanceIDType check instance id
type CheckInstanceIDType string

// CheckTargetType check target/host
type CheckTargetType string

// CheckSecretType check secret
type CheckSecretType string

// CheckTagsType check tags
type CheckTagsType string

// CheckDisplayNameType check display name
type CheckDisplayNameType string

// BrokerCNType broker common name
type BrokerCNType string

// CheckManager settings
type CheckManager struct {
	enabled bool
	Log     *log.Logger
	Debug   bool
	apih    *api.API

	initialized   bool
	initializedmu sync.RWMutex

	// check
	checkType             CheckTypeType
	checkID               api.IDType
	checkInstanceID       CheckInstanceIDType
	checkTarget           CheckTargetType
	checkSearchTag        api.TagType
	checkSecret           CheckSecretType
	checkTags             api.TagType
	customConfigFields    map[string]string
	checkSubmissionURL    api.URLType
	checkDisplayName      CheckDisplayNameType
	forceMetricActivation bool
	forceCheckUpdate      bool

	// metric tags
	metricTags map[string][]string
	mtmu       sync.Mutex

	// broker
	brokerID              api.IDType
	brokerSelectTag       api.TagType
	brokerMaxResponseTime time.Duration
	brokerTLS             *tls.Config

	// state
	checkBundle        *api.CheckBundle
	cbmu               sync.Mutex
	availableMetrics   map[string]bool
	availableMetricsmu sync.Mutex
	trapURL            api.URLType
	trapCN             BrokerCNType
	trapLastUpdate     time.Time
	trapMaxURLAge      time.Duration
	trapmu             sync.Mutex
	certPool           *x509.CertPool
	sockRx             *regexp.Regexp
}

// Trap config
type Trap struct {
	URL           *url.URL
	TLS           *tls.Config
	IsSocket      bool
	SockTransport *httpunix.Transport
}

// NewCheckManager returns a new check manager
func NewCheckManager(cfg *Config) (*CheckManager, error) {
	return New(cfg)
}

// New returns a new check manager
func New(cfg *Config) (*CheckManager, error) {

	if cfg == nil {
		return nil, errors.New("invalid Check Manager configuration (nil)")
	}

	cm := &CheckManager{enabled: true, initialized: false}

	// Setup logging for check manager
	cm.Debug = cfg.Debug
	cm.Log = cfg.Log
	if cm.Debug && cm.Log == nil {
		cm.Log = log.New(os.Stderr, "", log.LstdFlags)
	}
	if cm.Log == nil {
		cm.Log = log.New(ioutil.Discard, "", log.LstdFlags)
	}

	{
		rx, err := regexp.Compile(`^http\+unix://(?P<sockfile>.+)/write/(?P<id>.+)$`)
		if err != nil {
			return nil, errors.Wrap(err, "compiling socket regex")
		}
		cm.sockRx = rx
	}

	if cfg.Check.SubmissionURL != "" {
		cm.checkSubmissionURL = api.URLType(cfg.Check.SubmissionURL)
	}

	// Blank API Token *disables* check management
	if cfg.API.TokenKey == "" {
		cm.enabled = false
	}

	if !cm.enabled && cm.checkSubmissionURL == "" {
		return nil, errors.New("invalid check manager configuration (no API token AND no submission url)")
	}

	if cm.enabled {
		// initialize api handle
		cfg.API.Debug = cm.Debug
		cfg.API.Log = cm.Log
		apih, err := api.New(&cfg.API)
		if err != nil {
			return nil, errors.Wrap(err, "initializing api client")
		}
		cm.apih = apih
	}

	// initialize check related data
	if cfg.Check.Type != "" {
		cm.checkType = CheckTypeType(cfg.Check.Type)
	} else {
		cm.checkType = defaultCheckType
	}

	idSetting := "0"
	if cfg.Check.ID != "" {
		idSetting = cfg.Check.ID
	}
	id, err := strconv.Atoi(idSetting)
	if err != nil {
		return nil, errors.Wrap(err, "converting check id")
	}
	cm.checkID = api.IDType(id)

	cm.checkInstanceID = CheckInstanceIDType(cfg.Check.InstanceID)
	cm.checkTarget = CheckTargetType(cfg.Check.TargetHost)
	cm.checkDisplayName = CheckDisplayNameType(cfg.Check.DisplayName)
	cm.checkSecret = CheckSecretType(cfg.Check.Secret)

	fma := defaultForceMetricActivation
	if cfg.Check.ForceMetricActivation != "" {
		fma = cfg.Check.ForceMetricActivation
	}
	fm, err := strconv.ParseBool(fma)
	if err != nil {
		return nil, errors.Wrap(err, "parsing force metric activation")
	}
	cm.forceMetricActivation = fm

	_, an := path.Split(os.Args[0])
	hn, err := os.Hostname()
	if err != nil {
		hn = "unknown"
	}
	if cm.checkInstanceID == "" {
		cm.checkInstanceID = CheckInstanceIDType(fmt.Sprintf("%s:%s", hn, an))
	}
	if cm.checkDisplayName == "" {
		cm.checkDisplayName = CheckDisplayNameType(cm.checkInstanceID)
	}
	if cm.checkTarget == "" {
		cm.checkTarget = CheckTargetType(cm.checkInstanceID)
	}

	if cfg.Check.SearchTag == "" {
		cm.checkSearchTag = []string{fmt.Sprintf("service:%s", an)}
	} else {
		cm.checkSearchTag = strings.Split(strings.Replace(cfg.Check.SearchTag, " ", "", -1), ",")
	}

	if cfg.Check.Tags != "" {
		cm.checkTags = strings.Split(strings.Replace(cfg.Check.Tags, " ", "", -1), ",")
	}

	cm.customConfigFields = make(map[string]string)
	if len(cfg.Check.CustomConfigFields) > 0 {
		for fld, val := range cfg.Check.CustomConfigFields {
			cm.customConfigFields[fld] = val
		}
	}

	dur := cfg.Check.MaxURLAge
	if dur == "" {
		dur = defaultTrapMaxURLAge
	}
	maxDur, err := time.ParseDuration(dur)
	if err != nil {
		return nil, errors.Wrap(err, "parsing max url age")
	}
	cm.trapMaxURLAge = maxDur

	// setup broker
	idSetting = "0"
	if cfg.Broker.ID != "" {
		idSetting = cfg.Broker.ID
	}
	id, err = strconv.Atoi(idSetting)
	if err != nil {
		return nil, errors.Wrap(err, "parsing broker id")
	}
	cm.brokerID = api.IDType(id)

	if cfg.Broker.SelectTag != "" {
		cm.brokerSelectTag = strings.Split(strings.Replace(cfg.Broker.SelectTag, " ", "", -1), ",")
	}

	dur = cfg.Broker.MaxResponseTime
	if dur == "" {
		dur = defaultBrokerMaxResponseTime
	}
	maxDur, err = time.ParseDuration(dur)
	if err != nil {
		return nil, errors.Wrap(err, "parsing broker max response time")
	}
	cm.brokerMaxResponseTime = maxDur

	// add user specified tls config for broker if provided
	cm.brokerTLS = cfg.Broker.TLSConfig

	// metrics
	cm.availableMetrics = make(map[string]bool)
	cm.metricTags = make(map[string][]string)

	return cm, nil
}

// Initialize for sending metrics
func (cm *CheckManager) Initialize() {

	// if not managing the check, quicker initialization
	if !cm.enabled {
		err := cm.initializeTrapURL()
		if err == nil {
			cm.initializedmu.Lock()
			cm.initialized = true
			cm.initializedmu.Unlock()
		} else {
			cm.Log.Printf("[WARN] error initializing trap %s", err.Error())
		}
		return
	}

	// background initialization when we have to reach out to the api
	go func() {
		cm.apih.EnableExponentialBackoff()
		err := cm.initializeTrapURL()
		if err == nil {
			cm.initializedmu.Lock()
			cm.initialized = true
			cm.initializedmu.Unlock()
		} else {
			cm.Log.Printf("[WARN] error initializing trap %s", err.Error())
		}
		cm.apih.DisableExponentialBackoff()
	}()
}

// IsReady reflects if the check has been initialied and metrics can be sent to Circonus
func (cm *CheckManager) IsReady() bool {
	cm.initializedmu.RLock()
	defer cm.initializedmu.RUnlock()
	return cm.initialized
}

// GetSubmissionURL returns submission url for circonus
func (cm *CheckManager) GetSubmissionURL() (*Trap, error) {
	if cm.trapURL == "" {
		return nil, errors.Errorf("get submission url - submission url unavailable")
	}

	trap := &Trap{}

	u, err := url.Parse(string(cm.trapURL))
	if err != nil {
		return nil, errors.Wrap(err, "get submission url")
	}
	trap.URL = u

	if u.Scheme == "http+unix" {
		service := "circonus-agent"
		sockPath := ""
		metricID := ""

		subNames := cm.sockRx.SubexpNames()
		matches := cm.sockRx.FindAllStringSubmatch(string(cm.trapURL), -1)
		for _, match := range matches {
			for idx, val := range match {
				switch subNames[idx] {
				case "sockfile":
					sockPath = val
				case "id":
					metricID = val
				}
			}
		}

		if sockPath == "" || metricID == "" {
			return nil, errors.Errorf("get submission url - invalid socket url (%s)", cm.trapURL)
		}

		u, err = url.Parse(fmt.Sprintf("http+unix://%s/write/%s", service, metricID))
		if err != nil {
			return nil, errors.Wrap(err, "get submission url")
		}
		trap.URL = u

		trap.SockTransport = &httpunix.Transport{
			DialTimeout:           100 * time.Millisecond,
			RequestTimeout:        1 * time.Second,
			ResponseHeaderTimeout: 1 * time.Second,
		}
		trap.SockTransport.RegisterLocation(service, sockPath)
		trap.IsSocket = true
	}

	if u.Scheme == "https" {
		// preference user-supplied TLS configuration
		if cm.brokerTLS != nil {
			trap.TLS = cm.brokerTLS
			return trap, nil
		}

		// api.circonus.com uses a public CA signed certificate
		// trap.noit.circonus.net uses Circonus CA private certificate
		// enterprise brokers use private CA certificate
		if trap.URL.Hostname() == "api.circonus.com" {
			return trap, nil
		}

		if cm.certPool == nil {
			if err := cm.loadCACert(); err != nil {
				return nil, errors.Wrap(err, "get submission url")
			}
		}
		t := &tls.Config{
			RootCAs: cm.certPool,
		}
		if cm.trapCN != "" {
			t.ServerName = string(cm.trapCN)
		}
		trap.TLS = t
	}

	return trap, nil
}

// ResetTrap URL, force request to the API for the submission URL and broker ca cert
func (cm *CheckManager) ResetTrap() error {
	if cm.trapURL == "" {
		return nil
	}

	cm.trapURL = ""
	cm.certPool = nil // force re-fetching CA cert (if custom TLS config not supplied)
	return cm.initializeTrapURL()
}

// RefreshTrap check when the last time the URL was reset, reset if needed
func (cm *CheckManager) RefreshTrap() error {
	if cm.trapURL == "" {
		return nil
	}

	if time.Since(cm.trapLastUpdate) >= cm.trapMaxURLAge {
		return cm.ResetTrap()
	}

	return nil
}
