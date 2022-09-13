// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package envoy

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	envoyBootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	"github.com/hashicorp/go-multierror"
	"sigs.k8s.io/yaml"

	"istio.io/pkg/log"
)

const (
	// InvalidBaseID used to indicate that the Envoy BaseID has not been set. Attempting
	// to Close this BaseID will have no effect.
	InvalidBaseID = BaseID(math.MaxUint32)
)

// FlagName is the raw flag name passed to envoy.
type FlagName string

func (n FlagName) String() string {
	return string(n)
}

// Option for an Envoy Instance.
type Option interface {
	// FlagName returns the flag name used on the command line.
	FlagName() FlagName

	// FlagValue returns the flag value used on the command line. Can be empty for boolean flags.
	FlagValue() string

	apply(ctx *configContext)
	validate(ctx *configContext) error
}

type Options []Option

// ToArgs creates the command line arguments for the list of options.
func (options Options) ToArgs() []string {
	// Get the arguments for the command.
	args := make([]string, 0, len(options)*2)
	for _, o := range options {
		name := o.FlagName()
		if name != "" {
			args = append(args, name.String())
			value := o.FlagValue()
			if value != "" {
				args = append(args, value)
			}
		}
	}
	return args
}

// Validate the Options.
func (options Options) Validate() error {
	return options.validate(newConfigContext())
}

// validate is an internal method for validation.
func (options Options) validate(ctx *configContext) error {
	// Check for any duplicate user-specified options
	if err := options.checkDuplicates(); err != nil {
		return err
	}

	// Add a placeholder ConfigPath option. Can and should be overridden by user-provided value.
	// Used for force config validation to ensure that either configPath or configYaml has been set.
	opts := append(Options{ConfigPath("")}, options...)

	// Apply the options to the context.
	for _, o := range opts {
		o.apply(ctx)
	}

	// Validate all of the options.
	for _, o := range opts {
		if err := o.validate(ctx); err != nil {
			return err
		}
	}

	return nil
}

// checkDuplicates ensures to make sure that there are no duplicate options.
func (options Options) checkDuplicates() error {
	optionSet := make(map[FlagName]struct{})
	for _, o := range options {
		if _, ok := optionSet[o.FlagName()]; ok {
			return fmt.Errorf("multiple options specified for %s", o.FlagName())
		}
		optionSet[o.FlagName()] = struct{}{}
	}
	return nil
}

// NewOptions creates new Options from the given raw Envoy arguments. Returns an error if a problem
// was encountered while parsing the arguments.
func NewOptions(args ...string) (Options, error) {
	out := make(Options, 0, len(args))
	var next *genericOption
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if strings.HasPrefix(arg, "-") {
			// The argument is a new flag name.
			if next != nil {
				out = append(out, next)
			}

			flagName := FlagName(arg)

			if v, ok := flagValidators[flagName]; ok {
				// Known flag - use an existing validator.
				next = &genericOption{
					v: v,
				}
			} else {
				// Unknown flag - No validator.
				next = &genericOption{
					v: &flagValidator{
						flagName: flagName,
						apply:    func(*configContext, string) {},
						validate: func(*configContext, string) error {
							return nil
						},
					},
				}
			}
		} else {
			// The argument is a flag value.
			if next == nil {
				return nil, fmt.Errorf("raw argument missing flag name: %s", arg)
			}

			// Completed the current flag.
			next.value = arg
			out = append(out, next)
			next = nil
		}
	}

	if next != nil {
		out = append(out, next)
	}

	return out, nil
}

// LogLevel is an Option that sets the Envoy log level.
type LogLevel string

var _ Option = LogLevel("")

const (
	LogLevelTrace    LogLevel = "trace"
	LogLevelDebug    LogLevel = "debug"
	LogLevelInfo     LogLevel = "info"
	LogLevelWarning  LogLevel = "warning"
	LogLevelCritical LogLevel = "critical"
	LogLevelOff      LogLevel = "off"
)

func (l LogLevel) FlagName() FlagName {
	return logLevelValidator.flagName
}

func (l LogLevel) FlagValue() string {
	return string(l)
}

func (l LogLevel) apply(ctx *configContext) {
	logLevelValidator.apply(ctx, l.FlagValue())
}

func (l LogLevel) validate(ctx *configContext) error {
	return logLevelValidator.validate(ctx, l.FlagValue())
}

var logLevelValidator = registerFlagValidator(&flagValidator{
	flagName: "--log-level",
	apply: func(ctx *configContext, flagValue string) {
		// Do nothing.
	},
	validate: func(ctx *configContext, flagValue string) error {
		logLevel := LogLevel(flagValue)
		switch logLevel {
		case LogLevelTrace, LogLevelDebug, LogLevelInfo, LogLevelWarning, LogLevelCritical, LogLevelOff:
			return nil
		default:
			return fmt.Errorf("unsupported log level: %v", logLevel)
		}
	},
})

// ComponentLogLevel defines the log level for a single component.
type ComponentLogLevel struct {
	Name  string
	Level LogLevel
}

func (l ComponentLogLevel) String() string {
	return l.Name + ":" + string(l.Level)
}

// ParseComponentLogLevels parses the given envoy --component-log-level value string.
func ParseComponentLogLevels(value string) ComponentLogLevels {
	parts := strings.Split(value, ",")

	out := make(ComponentLogLevels, 0, len(parts))

	for _, part := range parts {
		keyAndValue := strings.Split(part, ":")
		if len(keyAndValue) == 2 {
			out = append(out, ComponentLogLevel{
				Name:  keyAndValue[0],
				Level: LogLevel(keyAndValue[1]),
			})
		}
	}

	return out
}

// ComponentLogLevels is an Option for multiple component log levels.
type ComponentLogLevels []ComponentLogLevel

var _ Option = ComponentLogLevels{}

func (l ComponentLogLevels) apply(ctx *configContext) {
	componentLogLevelValidator.apply(ctx, l.FlagValue())
}

func (l ComponentLogLevels) validate(ctx *configContext) error {
	return componentLogLevelValidator.validate(ctx, l.FlagValue())
}

func (l ComponentLogLevels) FlagName() FlagName {
	return componentLogLevelValidator.flagName
}

func (l ComponentLogLevels) FlagValue() string {
	strLevels := make([]string, 0, len(l))
	for _, cl := range l {
		strLevels = append(strLevels, cl.String())
	}
	return strings.Join(strLevels, ",")
}

var componentLogLevelValidator = registerFlagValidator(&flagValidator{
	flagName: "--component-log-level",
	apply: func(ctx *configContext, flagValue string) {
		// Do nothing.
	},
	validate: func(ctx *configContext, flagValue string) error {
		l := ParseComponentLogLevels(flagValue)
		for i, cl := range l {
			if cl.Name == "" {
				return fmt.Errorf("name is empty for component log level %d", i)
			}
			if err := cl.Level.validate(ctx); err != nil {
				return fmt.Errorf("level invalid for component log level %d: %v", i, err)
			}
		}
		return nil
	},
})

// IPVersion is an enumeration for IP versions for the --local-address-ip-version flag.
type IPVersion string

const (
	IPV4 IPVersion = "v4"
	IPV6 IPVersion = "v6"
)

// LocalAddressIPVersion sets the --local-address-ip-version flag, which sets the IP address
// version used for the local IP address. The default is V4.
func LocalAddressIPVersion(v IPVersion) Option {
	return &genericOption{
		value: string(v),
		v:     localAddressIPVersionValidator,
	}
}

var localAddressIPVersionValidator = registerFlagValidator(&flagValidator{
	flagName: "--local-address-ip-version",
	validate: func(ctx *configContext, flagValue string) error {
		ipVersion := IPVersion(flagValue)
		switch ipVersion {
		case IPV4, IPV6:
			return nil
		default:
			return fmt.Errorf("invalid LocalAddressIPVersion %v", ipVersion)
		}
	},
})

// ConfigPath sets the --config-path flag, which provides Envoy with the
// to the v2 bootstrap configuration file. If not set, ConfigYaml is required.
func ConfigPath(path string) Option {
	return &genericOption{
		value: path,
		v:     configPathValidator,
	}
}

var configPathValidator = registerFlagValidator(&flagValidator{
	flagName: "--config-path",
	apply: func(ctx *configContext, flagValue string) {
		ctx.configPath = flagValue
	},
	validate: func(ctx *configContext, flagValue string) error {
		// Ensure that either config path or configYaml is specified.
		if ctx.configPath == "" && ctx.configYaml == "" {
			return errors.New("must specify ConfigPath and/or ConfigYaml ")
		}
		if ctx.configPath != "" {
			// Check that the path set in the config exists.
			if _, err := os.Stat(ctx.configPath); os.IsNotExist(err) {
				return fmt.Errorf("configPath file does not exist: %s", ctx.configPath)
			}
		}
		return nil
	},
})

// ConfigYaml sets the --config-yaml flag, which provides Envoy with the
// a YAML string for a v2 bootstrap configuration. If ConfigPath is also set, the values in this
// YAML string will override and merge with the bootstrap loaded from ConfigPath.
func ConfigYaml(yaml string) Option {
	return &genericOption{
		value: yaml,
		v:     configYamlValidator,
	}
}

var configYamlValidator = registerFlagValidator(&flagValidator{
	flagName: "--config-yaml",
	apply: func(ctx *configContext, flagValue string) {
		ctx.configYaml = flagValue
	},
})

// BaseID is an Option that sets the --base-id flag. This is typically only needed when running multiple
// Envoys on the same machine (common in testing environments).
//
// Envoy will allocate shared memory if provided with a BaseID. This shared memory is used during hot restarts.
// It is up to the caller to free this memory by calling Close() on the BaseID when appropriate.
type BaseID uint32

var _ Option = BaseID(0)

func (bid BaseID) FlagName() FlagName {
	return baseIDValidator.flagName
}

func (bid BaseID) FlagValue() string {
	return strconv.FormatUint(uint64(bid), 10)
}

func (bid BaseID) apply(ctx *configContext) {
	baseIDValidator.apply(ctx, bid.FlagValue())
}

func (bid BaseID) validate(ctx *configContext) error {
	return baseIDValidator.validate(ctx, bid.FlagValue())
}

// GetInternalEnvoyValue returns the value used internally by Envoy.
func (bid BaseID) GetInternalEnvoyValue() uint64 {
	return uint64(bid)
}

// Close removes the shared memory allocated by Envoy for this BaseID.
func (bid BaseID) Close() error {
	if bid != InvalidBaseID {
		// Envoy internally multiplies the base ID from the command line by 10 so that they have spread
		// for domain sockets.
		path := fmt.Sprintf("/dev/shm/envoy_shared_memory_%d", bid.GetInternalEnvoyValue())
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("error deleting Envoy base ID %d shared memory %s: %v", bid, path, err)
		}
		log.Debugf("successfully freed Envoy base ID %d shared memory %s", bid, path)
	}
	return nil
}

var baseIDValidator = registerFlagValidator(&flagValidator{
	flagName: "--base-id",
	apply: func(ctx *configContext, flagValue string) {
		if bid, err := strconv.ParseUint(flagValue, 10, 64); err == nil {
			ctx.baseID = BaseID(bid)
		}
	},
	validate: func(ctx *configContext, flagValue string) error {
		if _, err := strconv.ParseUint(flagValue, 10, 64); err != nil {
			return err
		}
		return nil
	},
})

// GenerateBaseID is a method copied from Envoy server tests.
//
// Computes a numeric ID to incorporate into the names of shared-memory segments and
// domain sockets, to help keep them distinct from other tests that might be running concurrently.
func GenerateBaseID() BaseID {
	// The PID is needed to isolate namespaces between concurrent processes in CI.
	pid := uint32(os.Getpid())

	// A random number is needed to avoid BaseID collisions for multiple Envoys started from the same
	// process.
	randNum := rand.Uint32()

	// Pick a prime number to give more of the 32-bits of entropy to the PID, and the
	// remainder to the random number.
	fourDigitPrime := uint32(7919)
	value := pid*fourDigitPrime + randNum%fourDigitPrime

	// TODO(nmittler): Limit to uint16 - Large values seem to cause unexpected shared memory paths in envoy.
	out := BaseID(value % math.MaxUint16)
	return out
}

// Concurrency sets the --concurrency flag, which sets the number of worker threads to run.
func Concurrency(concurrency uint16) Option {
	return &genericOption{
		value: strconv.FormatUint(uint64(concurrency), 10),
		v:     concurrencyValidator,
	}
}

var concurrencyValidator = registerFlagValidator(&flagValidator{
	flagName: "--concurrency",
	validate: func(ctx *configContext, flagValue string) error {
		if _, err := strconv.ParseUint(flagValue, 10, 64); err != nil {
			return err
		}
		return nil
	},
})

// DisableHotRestart sets the --disable-hot-restart flag.
func DisableHotRestart(disable bool) Option {
	var v *flagValidator
	if disable {
		v = disableHotRestartValidator
	}

	return &genericOption{
		v:     v,
		value: "",
	}
}

var disableHotRestartValidator = registerBoolFlagValidator("--disable-hot-restart")

// LogPath sets the --log-path flag, which specifies the output file for logs. If not set
// logs will be written to stderr.
func LogPath(path string) Option {
	return &genericOption{
		value: path,
		v:     logPathValidator,
	}
}

var logPathValidator = registerFlagValidator(&flagValidator{
	flagName: "--log-path",
})

// LogFormat sets the --log-format flag, which specifies the format string to use for log
// messages.
func LogFormat(format string) Option {
	return &genericOption{
		value: format,
		v:     logFormatValidator,
	}
}

var logFormatValidator = registerFlagValidator(&flagValidator{
	flagName: "--log-format",
})

// ServiceCluster sets the --service-cluster flag, which defines the local service cluster
// name where Envoy is running
func ServiceCluster(c string) Option {
	return &genericOption{
		value: c,
		v:     serviceClusterValidator,
	}
}

var serviceClusterValidator = registerFlagValidator(&flagValidator{
	flagName: "--service-cluster",
})

// ServiceNode sets the --service-node flag, which defines the local service node name
// where Envoy is running
func ServiceNode(n string) Option {
	return &genericOption{
		value: n,
		v:     serviceNodeValidator,
	}
}

var serviceNodeValidator = registerFlagValidator(&flagValidator{
	flagName: "--service-node",
})

// DrainDuration sets the --drain-time-s flag, which defines the amount of time that Envoy will
// drain connections during a hot restart.
func DrainDuration(duration time.Duration) Option {
	return &genericOption{
		value: strconv.Itoa(int(duration / time.Second)),
		v:     drainDurationValidator,
	}
}

var drainDurationValidator = registerFlagValidator(&flagValidator{
	flagName: "--drain-time-s",
	validate: func(ctx *configContext, flagValue string) error {
		if _, err := strconv.ParseUint(flagValue, 10, 32); err != nil {
			return err
		}
		return nil
	},
})

// ParentShutdownDuration sets the --parent-shutdown-time-s flag, which defines the amount of
// time that Envoy will wait before shutting down the parent process during a hot restart
func ParentShutdownDuration(duration time.Duration) Option {
	return &genericOption{
		value: strconv.Itoa(int(duration / time.Second)),
		v:     parentShutdownDurationValidator,
	}
}

var parentShutdownDurationValidator = registerFlagValidator(&flagValidator{
	flagName: "--parent-shutdown-time-s",
	validate: func(ctx *configContext, flagValue string) error {
		if _, err := strconv.ParseUint(flagValue, 10, 32); err != nil {
			return err
		}
		return nil
	},
})

func registerBoolFlagValidator(flagName string) *flagValidator {
	return registerFlagValidator(&flagValidator{
		flagName: FlagName(flagName),
		validate: func(ctx *configContext, flagValue string) error {
			switch flagValue {
			case "", "true":
				return nil
			default:
				return fmt.Errorf("unexpected boolean value for flag %s: %s", flagName, flagValue)
			}
		},
	})
}

func getAdminPortFromYaml(yamlData string) (uint32, error) {
	jsonData, err := yaml.YAMLToJSON([]byte(yamlData))
	if err != nil {
		return 0, fmt.Errorf("error converting envoy bootstrap YAML to JSON: %v", err)
	}

	bootstrap := &envoyBootstrap.Bootstrap{}
	if err := unmarshal(string(jsonData), bootstrap); err != nil {
		return 0, fmt.Errorf("error parsing Envoy bootstrap JSON: %v", err)
	}
	if bootstrap.GetAdmin() == nil {
		return 0, errors.New("unable to locate admin in envoy bootstrap")
	}
	if bootstrap.GetAdmin().GetAddress() == nil {
		return 0, errors.New("unable to locate admin/address in envoy bootstrap")
	}
	if bootstrap.GetAdmin().GetAddress().GetSocketAddress() == nil {
		return 0, errors.New("unable to locate admin/address/socket_address in envoy bootstrap")
	}
	if bootstrap.GetAdmin().GetAddress().GetSocketAddress().GetPortValue() == 0 {
		return 0, errors.New("unable to locate admin/address/socket_address/port_value in envoy bootstrap")
	}
	return bootstrap.GetAdmin().GetAddress().GetSocketAddress().GetPortValue(), nil
}

// configContext stores the output of applied Options.
type configContext struct {
	configPath string
	configYaml string
	baseID     BaseID
}

func newConfigContext() *configContext {
	return &configContext{
		baseID: InvalidBaseID,
	}
}

func (c *configContext) getAdminPort() (uint32, error) {
	var err error

	// First, check the config yaml, which overrides config-path.
	if c.configYaml != "" {
		if port, e := getAdminPortFromYaml(c.configYaml); e != nil {
			err = fmt.Errorf("failed to locate admin port in envoy config-yaml: %v", e)
		} else {
			// Found the port!
			return port, nil
		}
	}

	// Haven't found it yet - check configPath.
	if c.configPath == "" {
		return 0, multierror.Append(err, errors.New("unable to process envoy bootstrap"))
	}

	content, e := os.ReadFile(c.configPath)
	if e != nil {
		return 0, multierror.Append(err, fmt.Errorf("failed reading config-path file %s: %v", c.configPath, e))
	}

	port, e := getAdminPortFromYaml(string(content))
	if e != nil {
		return 0, multierror.Append(err, fmt.Errorf("failed to locate admin port in envoy config-yaml: %v", e))
	}
	// Found the port!
	return port, nil
}

var flagValidators = make(map[FlagName]*flagValidator)

type flagValidator struct {
	flagName FlagName
	apply    func(ctx *configContext, flagValue string)
	validate func(ctx *configContext, flagValue string) error
}

func registerFlagValidator(v *flagValidator) *flagValidator {
	flagValidators[v.flagName] = v

	// Fill in missing methods with defaults.
	if v.apply == nil {
		v.apply = func(*configContext, string) {}
	}
	if v.validate == nil {
		v.validate = func(*configContext, string) error {
			return nil
		}
	}
	return v
}

var _ Option = &genericOption{}

type genericOption struct {
	v     *flagValidator
	value string
}

func (o *genericOption) FlagName() FlagName {
	if o.v != nil {
		return o.v.flagName
	}
	return ""
}

func (o *genericOption) FlagValue() string {
	if o.v != nil {
		return o.value
	}
	return ""
}

func (o *genericOption) apply(ctx *configContext) {
	if o.v != nil && o.v.apply != nil {
		o.v.apply(ctx, o.value)
	}
}

func (o *genericOption) validate(ctx *configContext) error {
	if o.v != nil && o.v.validate != nil {
		return o.v.validate(ctx, o.value)
	}
	return nil
}
