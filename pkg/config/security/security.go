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

package security

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

// JwksInfo provides values resulting from parsing a jwks URI.
type JwksInfo struct {
	Hostname host.Name
	Scheme   string
	Port     int
	UseSSL   bool
}

const (
	attrRequestHeader       = "request.headers"                     // header name is surrounded by brackets, e.g. "request.headers[User-Agent]".
	attrRequestInlineHeader = "request.experimental.inline.headers" // header name is surrounded by brackets, e.g. "request.experimental.inline.headers[User-Agent]".
	attrSrcIP               = "source.ip"                           // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrRemoteIP            = "remote.ip"                           // original client ip determined from x-forwarded-for or proxy protocol.
	attrSrcNamespace        = "source.namespace"                    // e.g. "default".
	attrSrcServiceAccount   = "source.serviceAccount"               // e.g. "default/productpage".
	attrSrcPrincipal        = "source.principal"                    // source identity, e,g, "cluster.local/ns/default/sa/productpage".
	attrRequestPrincipal    = "request.auth.principal"              // authenticated principal of the request.
	attrRequestAudiences    = "request.auth.audiences"              // intended audience(s) for this authentication information.
	attrRequestPresenter    = "request.auth.presenter"              // authorized presenter of the credential.
	attrRequestClaims       = "request.auth.claims"                 // claim name is surrounded by brackets, e.g. "request.auth.claims[iss]".
	attrDestIP              = "destination.ip"                      // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrDestPort            = "destination.port"                    // must be in the range [0, 65535].
	attrDestLabel           = "destination.labels"                  // label name is surrounded by brackets, e.g. "destination.labels[version]".
	attrDestName            = "destination.name"                    // short service name, e.g. "productpage".
	attrDestNamespace       = "destination.namespace"               // e.g. "default".
	attrDestUser            = "destination.user"                    // service account, e.g. "bookinfo-productpage".
	attrConnSNI             = "connection.sni"                      // server name indication, e.g. "www.example.com".
	attrExperimental        = "experimental.envoy.filters."
)

var (
	MatchOneTemplate = "{*}"
	MatchAnyTemplate = "{**}"

	// Valid pchar from https://datatracker.ietf.org/doc/html/rfc3986#appendix-A
	// pchar = unreserved / pct-encoded / sub-delims / ":" / "@"
	validLiteral = regexp.MustCompile("^[a-zA-Z0-9-._~%!$&'()+,;:@=]+$")
)

// ParseJwksURI parses the input URI and returns the corresponding hostname, port, and whether SSL is used.
// URI must start with "http://" or "https://", which corresponding to "http" or "https" scheme.
// Port number is extracted from URI if available (i.e from postfix :<port>, eg. ":80"), or assigned
// to a default value based on URI scheme (80 for http and 443 for https).
// Port name is set to URI scheme value.
func ParseJwksURI(jwksURI string) (JwksInfo, error) {
	u, err := url.Parse(jwksURI)
	if err != nil {
		return JwksInfo{}, err
	}
	info := JwksInfo{}
	switch u.Scheme {
	case "http":
		info.UseSSL = false
		info.Port = 80
	case "https":
		info.UseSSL = true
		info.Port = 443
	default:
		return JwksInfo{}, fmt.Errorf("URI scheme %q is not supported", u.Scheme)
	}

	if u.Port() != "" {
		info.Port, err = strconv.Atoi(u.Port())
		if err != nil {
			return JwksInfo{}, err
		}
	}
	info.Hostname = host.Name(u.Hostname())
	info.Scheme = u.Scheme

	return info, nil
}

func CheckEmptyValues(key string, values []string) error {
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("empty value not allowed, found in %s", key)
		}
	}
	return nil
}

func CheckServiceAccount(key string, values []string) error {
	if len(values) > 16 {
		// Arbitrary limit to avoid unbounded configuration sizes
		return fmt.Errorf("may not have more than 16 values")
	}
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("empty value not allowed, found in %s", key)
		}
		if strings.Contains(value, "*") {
			return fmt.Errorf("wildcard not allowed, found in %s", key)
		}
		segments := strings.Count(value, "/")
		if segments != 0 && segments != 1 {
			return fmt.Errorf("expected format 'serviceAccount' or 'namespace/serviceAccount', found %q in %s", value, key)
		}
		if len(value) > 320 {
			return fmt.Errorf("value cannot exceed 320 characters, found %q in %s", value, key)
		}
		ns, sa, ok := strings.Cut(value, "/")
		if ok {
			if len(ns) == 0 {
				return fmt.Errorf("expected format 'serviceAccount' or 'namespace/serviceAccount', found empty namespace %q in %s", value, key)
			}
			if len(sa) == 0 {
				return fmt.Errorf("expected format 'serviceAccount' or 'namespace/serviceAccount', found empty serviceAccount %q in %s", value, key)
			}
		} else {
			sa := value
			if len(sa) == 0 {
				return fmt.Errorf("expected format 'serviceAccount' or 'namespace/serviceAccount', found empty serviceAccount %q in %s", value, key)
			}
		}
	}
	return nil
}

func CheckValidPathTemplate(key string, paths []string) error {
	for _, path := range paths {
		containsPathTemplate := ContainsPathTemplate(path)
		foundMatchAnyTemplate := false
		// Strip leading and trailing slashes if they exist
		path = strings.Trim(path, "/")
		globs := strings.Split(path, "/")
		for _, glob := range globs {
			// If glob is a supported path template, skip the check
			// If glob is {**}, it must be the last operator in the template
			if glob == MatchOneTemplate && !foundMatchAnyTemplate {
				continue
			} else if glob == MatchAnyTemplate && !foundMatchAnyTemplate {
				foundMatchAnyTemplate = true
				continue
			} else if (glob == MatchAnyTemplate || glob == MatchOneTemplate) && foundMatchAnyTemplate {
				return fmt.Errorf("invalid or unsupported path %s, found in %s. "+
					"{**} is not the last operator", path, key)
			}

			// If glob is not a supported path template and contains `{`, or `}` it is invalid.
			// Path is invalid if it contains `{` or `}` beyond a supported path template.
			if strings.ContainsAny(glob, "{}") {
				return fmt.Errorf("invalid or unsupported path %s, found in %s. "+
					"Contains '{' or '}' beyond a supported path template", path, key)
			}

			// Validate glob is valid string literal
			// Meets Envoy's valid pchar requirements from https://datatracker.ietf.org/doc/html/rfc3986#appendix-A
			if containsPathTemplate && !IsValidLiteral(glob) {
				return fmt.Errorf("invalid or unsupported path %s, found in %s. "+
					"Contains segment %s with invalid string literal", path, key, glob)
			}
		}
	}
	return nil
}

// IsValidLiteral returns true if the glob is a valid string literal.
func IsValidLiteral(glob string) bool {
	return validLiteral.MatchString(glob)
}

// ContainsPathTemplate returns true if the path contains a valid path template.
func ContainsPathTemplate(value string) bool {
	return strings.Contains(value, MatchOneTemplate) || strings.Contains(value, MatchAnyTemplate)
}

func ValidateAttribute(key string, values []string) error {
	if err := CheckEmptyValues(key, values); err != nil {
		return err
	}
	switch {
	case hasPrefix(key, attrRequestHeader):
		return validateMapKey(key)
	case hasPrefix(key, attrRequestInlineHeader):
		return validateMapKey(key)
	case isEqual(key, attrSrcIP):
		return ValidateIPs(values)
	case isEqual(key, attrRemoteIP):
		return ValidateIPs(values)
	case isEqual(key, attrSrcNamespace):
	case isEqual(key, attrSrcServiceAccount):
		return CheckServiceAccount(key, values)
	case isEqual(key, attrSrcPrincipal):
	case isEqual(key, attrRequestPrincipal):
	case isEqual(key, attrRequestAudiences):
	case isEqual(key, attrRequestPresenter):
	case hasPrefix(key, attrRequestClaims):
		return validateMapKey(key)
	case isEqual(key, attrDestIP):
		return ValidateIPs(values)
	case isEqual(key, attrDestPort):
		return ValidatePorts(values)
	case isEqual(key, attrConnSNI):
	case hasPrefix(key, attrExperimental):
		return validateMapKey(key)
	case isEqual(key, attrDestNamespace):
		return fmt.Errorf("attribute %s is replaced by the metadata.namespace", key)
	case hasPrefix(key, attrDestLabel):
		return fmt.Errorf("attribute %s is replaced by the workload selector", key)
	case isEqual(key, attrDestName, attrDestUser):
		return fmt.Errorf("deprecated attribute %s: only supported in v1alpha1", key)
	default:
		return fmt.Errorf("unknown attribute: %s", key)
	}
	return nil
}

func isEqual(key string, values ...string) bool {
	for _, v := range values {
		if key == v {
			return true
		}
	}
	return false
}

func hasPrefix(key string, prefix string) bool {
	return strings.HasPrefix(key, prefix)
}

func ValidateIPs(ips []string) error {
	var errs *multierror.Error
	for _, v := range ips {
		if strings.Contains(v, "/") {
			if _, err := netip.ParsePrefix(v); err != nil {
				errs = multierror.Append(errs, fmt.Errorf("bad CIDR range (%s): %v", v, err))
			}
		} else {
			if _, err := netip.ParseAddr(v); err != nil {
				errs = multierror.Append(errs, fmt.Errorf("bad IP address (%s)", v))
			}
		}
	}
	return errs.ErrorOrNil()
}

func ValidatePorts(ports []string) error {
	var errs *multierror.Error
	for _, port := range ports {
		p, err := strconv.ParseUint(port, 10, 32)
		if err != nil || p > 65535 {
			errs = multierror.Append(errs, fmt.Errorf("bad port (%s): %v", port, err))
		}
	}
	return errs.ErrorOrNil()
}

func validateMapKey(key string) error {
	open := strings.Index(key, "[")
	if strings.HasSuffix(key, "]") && open > 0 && open < len(key)-2 {
		return nil
	}
	return fmt.Errorf("bad key (%s): should have format a[b]", key)
}

// ValidCipherSuites contains a list of all ciphers supported in Gateway.server.tls.cipherSuites
// Extracted from: `bssl ciphers -openssl-name ALL | rg -v PSK`
var ValidCipherSuites = sets.New(
	"ECDHE-ECDSA-AES128-GCM-SHA256",
	"ECDHE-RSA-AES128-GCM-SHA256",
	"ECDHE-ECDSA-AES256-GCM-SHA384",
	"ECDHE-RSA-AES256-GCM-SHA384",
	"ECDHE-ECDSA-CHACHA20-POLY1305",
	"ECDHE-RSA-CHACHA20-POLY1305",
	"ECDHE-ECDSA-AES128-SHA",
	"ECDHE-RSA-AES128-SHA",
	"ECDHE-ECDSA-AES256-SHA",
	"ECDHE-RSA-AES256-SHA",
	"AES128-GCM-SHA256",
	"AES256-GCM-SHA384",
	"AES128-SHA",
	"AES256-SHA",
	"DES-CBC3-SHA",
)

// ValidECDHCurves contains a list of all ecdh curves supported in MeshConfig.TlsDefaults.ecdhCurves
// Source:
// https://github.com/google/boringssl/blob/58f3bc83230d2958bb9710bc910972c4f5d382dc/ssl/ssl_key_share.cc#L376-L385
var ValidECDHCurves = sets.New(
	"P-224",
	"P-256",
	"P-521",
	"P-384",
	"X25519",
	"X25519Kyber768Draft00",
	"X25519MLKEM768",
)

func IsValidCipherSuite(cs string) bool {
	if cs == "" || cs == "ALL" {
		return true
	}
	if !unicode.IsNumber(rune(cs[0])) && !unicode.IsLetter(rune(cs[0])) {
		// Not all of these are correct, but this is needed to support advanced cases like - and + operators
		// without needing to parse the full expression
		return true
	}
	return ValidCipherSuites.Contains(cs)
}

func IsValidECDHCurve(cs string) bool {
	if cs == "" {
		return true
	}
	return ValidECDHCurves.Contains(cs)
}

// FilterCipherSuites filters out invalid cipher suites which would lead Envoy to NACKing.
func FilterCipherSuites(suites []string) []string {
	if len(suites) == 0 {
		return nil
	}
	ret := make([]string, 0, len(suites))
	validCiphers := sets.New[string]()
	for _, s := range suites {
		if IsValidCipherSuite(s) {
			if !validCiphers.InsertContains(s) {
				ret = append(ret, s)
			} else if log.DebugEnabled() {
				log.Debugf("ignoring duplicated cipherSuite: %q", s)
			}
		} else if log.DebugEnabled() {
			log.Debugf("ignoring unsupported cipherSuite: %q", s)
		}
	}
	return ret
}
