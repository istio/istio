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

package envoyfilter

import (
	"fmt"
	"regexp"
	"strings"

	udpaa "github.com/cncf/xds/go/udpa/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/anypb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/config/xds"
	"istio.io/istio/pkg/wellknown"
)

type (
	Warning    = validation.Warning
	Validation = validation.Validation
)

// ValidateEnvoyFilter checks envoy filter config supplied by user
var ValidateEnvoyFilter = validation.RegisterValidateFunc("ValidateEnvoyFilter",
	func(cfg config.Config) (Warning, error) {
		errs := Validation{}
		errs = validation.AppendWarningf(errs, "EnvoyFilter exposes internal implementation details that may change at any time. "+
			"Prefer other APIs if possible, and exercise extreme caution, especially around upgrades.")
		return validateEnvoyFilter(cfg, errs)
	})

func validateEnvoyFilter(cfg config.Config, errs Validation) (Warning, error) {
	rule, ok := cfg.Spec.(*networking.EnvoyFilter)
	if !ok {
		return nil, fmt.Errorf("cannot cast to Envoy filter")
	}
	warning, err := validation.ValidateAlphaWorkloadSelector(rule.WorkloadSelector)
	if err != nil {
		return nil, err
	}

	// If workloadSelector is defined and labels are not set, it is most likely
	// an user error. Marking it as a warning to keep it backwards compatible.
	if warning != nil {
		errs = validation.AppendValidation(errs,
			validation.WrapWarning(fmt.Errorf("Envoy filter: %s, will be applied to all services in namespace", warning))) // nolint: stylecheck
	}

	for _, cp := range rule.ConfigPatches {
		if cp == nil {
			errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: null config patch")) // nolint: stylecheck
			continue
		}
		if cp.ApplyTo == networking.EnvoyFilter_INVALID {
			errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: missing applyTo")) // nolint: stylecheck
			continue
		}
		if cp.Patch == nil {
			errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: missing patch")) // nolint: stylecheck
			continue
		}
		if cp.Patch.Operation == networking.EnvoyFilter_Patch_INVALID {
			errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: missing patch operation")) // nolint: stylecheck
			continue
		}
		if cp.Patch.Operation != networking.EnvoyFilter_Patch_REMOVE && cp.Patch.Value == nil {
			errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: missing patch value for non-remove operation")) // nolint: stylecheck
			continue
		}

		// ensure that the supplied regex for proxy version compiles
		if cp.Match != nil && cp.Match.Proxy != nil && cp.Match.Proxy.ProxyVersion != "" {
			if _, err := regexp.Compile(cp.Match.Proxy.ProxyVersion); err != nil {
				errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: invalid regex for proxy version, [%v]", err)) // nolint: stylecheck
				continue
			}
		}
		// ensure that applyTo, match and patch all line up
		switch cp.ApplyTo {
		case networking.EnvoyFilter_LISTENER,
			networking.EnvoyFilter_FILTER_CHAIN,
			networking.EnvoyFilter_NETWORK_FILTER,
			networking.EnvoyFilter_HTTP_FILTER:
			if cp.Match != nil && cp.Match.ObjectTypes != nil {
				if cp.Match.GetListener() == nil {
					errs = validation.AppendValidation(errs,
						fmt.Errorf("Envoy filter: applyTo for listener class objects cannot have non listener match")) // nolint: stylecheck
					continue
				}
				listenerMatch := cp.Match.GetListener()
				if listenerMatch.FilterChain != nil {
					if listenerMatch.FilterChain.Filter != nil {
						if cp.ApplyTo == networking.EnvoyFilter_LISTENER || cp.ApplyTo == networking.EnvoyFilter_FILTER_CHAIN {
							// This would be an error but is a warning for backwards compatibility
							errs = validation.AppendValidation(errs, validation.WrapWarning(
								fmt.Errorf("Envoy filter: filter match has no effect when used with %v", cp.ApplyTo))) // nolint: stylecheck
						}
						// filter names are required if network filter matches are being made
						if listenerMatch.FilterChain.Filter.Name == "" {
							errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: filter match has no name to match on")) // nolint: stylecheck
							continue
						} else if listenerMatch.FilterChain.Filter.SubFilter != nil {
							// sub filter match is supported only for applyTo HTTP_FILTER
							if cp.ApplyTo != networking.EnvoyFilter_HTTP_FILTER {
								errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: subfilter match can be used with applyTo HTTP_FILTER only")) // nolint: stylecheck
								continue
							}
							// sub filter match requires the network filter to match to envoy http connection manager
							if listenerMatch.FilterChain.Filter.Name != wellknown.HTTPConnectionManager &&
								listenerMatch.FilterChain.Filter.Name != "envoy.http_connection_manager" {
								errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: subfilter match requires filter match with %s", // nolint: stylecheck
									wellknown.HTTPConnectionManager))
								continue
							}
							if listenerMatch.FilterChain.Filter.SubFilter.Name == "" {
								errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: subfilter match has no name to match on")) // nolint: stylecheck
								continue
							}
						}
						errs = validation.AppendValidation(errs, validateListenerMatchName(listenerMatch.FilterChain.Filter.GetName()))
						errs = validation.AppendValidation(errs, validateListenerMatchName(listenerMatch.FilterChain.Filter.GetSubFilter().GetName()))
					}
				}
			}
		case networking.EnvoyFilter_ROUTE_CONFIGURATION, networking.EnvoyFilter_VIRTUAL_HOST, networking.EnvoyFilter_HTTP_ROUTE:
			if cp.Match != nil && cp.Match.ObjectTypes != nil {
				if cp.Match.GetRouteConfiguration() == nil {
					errs = validation.AppendValidation(errs,
						fmt.Errorf("Envoy filter: applyTo for http route class objects cannot have non route configuration match")) // nolint: stylecheck
				}
			}

		case networking.EnvoyFilter_CLUSTER:
			if cp.Match != nil && cp.Match.ObjectTypes != nil {
				if cp.Match.GetCluster() == nil {
					errs = validation.AppendValidation(errs, fmt.Errorf("Envoy filter: applyTo for cluster class objects cannot have non cluster match")) // nolint: stylecheck
				}
			}
		}
		// ensure that the struct is valid
		if _, err := xds.BuildXDSObjectFromStruct(cp.ApplyTo, cp.Patch.Value, false); err != nil {
			if strings.Contains(err.Error(), "could not resolve Any message type") {
				if strings.Contains(err.Error(), ".v2.") {
					err = fmt.Errorf("referenced type unknown (hint: try using the v3 XDS API): %v", err)
				} else {
					err = fmt.Errorf("referenced type unknown: %v", err)
				}
			}
			errs = validation.AppendValidation(errs, err)
		} else {
			// Run with strict validation, and emit warnings. This helps capture cases like unknown fields
			// We do not want to reject in case the proto is valid but our libraries are outdated
			obj, err := xds.BuildXDSObjectFromStruct(cp.ApplyTo, cp.Patch.Value, true)
			if err != nil {
				errs = validation.AppendValidation(errs, validation.WrapWarning(err))
			}

			// Append any deprecation notices
			if obj != nil {
				// Note: since we no longer import v2 protos, v2 references will fail during BuildXDSObjectFromStruct.
				errs = validation.AppendValidation(errs, validateDeprecatedFilterTypes(obj))
				errs = validation.AppendValidation(errs, validateMissingTypedConfigFilterTypes(obj))
			}
		}
	}

	return errs.Unwrap()
}

func validateListenerMatchName(name string) error {
	if newName, f := xds.ReverseDeprecatedFilterNames[name]; f {
		return validation.WrapWarning(fmt.Errorf("using deprecated filter name %q; use %q instead", name, newName))
	}
	return nil
}

func recurseDeprecatedTypes(message protoreflect.Message) ([]string, error) {
	var topError error
	var deprecatedTypes []string
	if message == nil {
		return nil, nil
	}
	message.Range(func(descriptor protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		m, isMessage := value.Interface().(protoreflect.Message)
		if isMessage {
			anyMessage, isAny := m.Interface().(*anypb.Any)
			if isAny {
				mt, err := protoregistry.GlobalTypes.FindMessageByURL(anyMessage.TypeUrl)
				if err != nil {
					topError = err
					return false
				}
				var fileOpts proto.Message = mt.Descriptor().ParentFile().Options().(*descriptorpb.FileOptions)
				if proto.HasExtension(fileOpts, udpaa.E_FileStatus) {
					ext := proto.GetExtension(fileOpts, udpaa.E_FileStatus)
					udpaext, ok := ext.(*udpaa.StatusAnnotation)
					if !ok {
						topError = fmt.Errorf("extension was of wrong type: %T", ext)
						return false
					}
					if udpaext.PackageVersionStatus == udpaa.PackageVersionStatus_FROZEN {
						deprecatedTypes = append(deprecatedTypes, anyMessage.TypeUrl)
					}
				}
			}
			newTypes, err := recurseDeprecatedTypes(m)
			if err != nil {
				topError = err
				return false
			}
			deprecatedTypes = append(deprecatedTypes, newTypes...)
		}
		return true
	})
	return deprecatedTypes, topError
}

// recurseMissingTypedConfig checks that configured filters do not rely on `name` and elide `typed_config`.
// This is temporarily enabled in Envoy by the envoy.reloadable_features.no_extension_lookup_by_name flag, but in the future will be removed.
func recurseMissingTypedConfig(message protoreflect.Message) []string {
	var deprecatedTypes []string
	if message == nil {
		return nil
	}
	// First, iterate over the fields to find the 'name' field to help with reporting errors.
	var name string
	for i := 0; i < message.Type().Descriptor().Fields().Len(); i++ {
		field := message.Type().Descriptor().Fields().Get(i)
		if field.JSONName() == "name" {
			name = fmt.Sprintf("%v", message.Get(field).Interface())
		}
	}

	hasTypedConfig := false
	requiresTypedConfig := false
	// Now go through fields again
	for i := 0; i < message.Type().Descriptor().Fields().Len(); i++ {
		field := message.Type().Descriptor().Fields().Get(i)
		set := message.Has(field)
		// If it has a typedConfig field, it must be set.
		requiresTypedConfig = requiresTypedConfig || field.JSONName() == "typedConfig"
		// Note: it is possible there is some API that has typedConfig but has a non-deprecated alternative,
		// but I couldn't find any. Worst case, this is a warning, not an error, so a false positive is not so bad.
		// The one exception is configDiscovery (used for ECDS)
		if field.JSONName() == "typedConfig" && set {
			hasTypedConfig = true
		}
		if field.JSONName() == "configDiscovery" && set {
			hasTypedConfig = true
		}
		if set {
			// If the field was set and is a message, recurse into it to check children
			m, isMessage := message.Get(field).Interface().(protoreflect.Message)
			if isMessage {
				deprecatedTypes = append(deprecatedTypes, recurseMissingTypedConfig(m)...)
			}
		}
	}
	if requiresTypedConfig && !hasTypedConfig {
		deprecatedTypes = append(deprecatedTypes, name)
	}
	return deprecatedTypes
}

func validateDeprecatedFilterTypes(obj proto.Message) error {
	deprecated, err := recurseDeprecatedTypes(obj.ProtoReflect())
	if err != nil {
		return fmt.Errorf("failed to find deprecated types: %v", err)
	}
	if len(deprecated) > 0 {
		return validation.WrapWarning(fmt.Errorf("using deprecated type_url(s); %v", strings.Join(deprecated, ", ")))
	}
	return nil
}

func validateMissingTypedConfigFilterTypes(obj proto.Message) error {
	missing := recurseMissingTypedConfig(obj.ProtoReflect())
	if len(missing) > 0 {
		return validation.WrapWarning(fmt.Errorf("using deprecated types by name without typed_config; %v", strings.Join(missing, ", ")))
	}
	return nil
}
