package cluster

import "strings"

const (
	// Latest represents a version of Istio running from master
	Latest = "default"
)

// Version is an Istio version running within a cluster
type Version string

// Versions represents a collection of Istio versions running in a cluster
type Versions []Version

// ToRevision goes from an Istio version to the canonical revision for that version
func (v Version) ToRevision() string {
	ver := string(v)
	if ver == Latest {
		return Latest
	}
	return strings.ReplaceAll(string(v), ".", "-")
}

// ToRevisions returns the list of canonical revisions for a set of versions
func (v Versions) ToRevisions() []string {
	revs := make([]string, len(v))
	for i, ver := range v {
		revs[i] = ver.ToRevision()
	}
	return revs
}

// ParseVersions attempts to construct Versions from a string slice
func ParseVersions(versions []string) (Versions, error) {
	vers := make([]Version, len(versions))
	for i, v := range versions {
		parsedVer, err := parseVersion(v)
		if err != nil {
			return nil, err
		}
		vers[i] = parsedVer
	}
	return vers, nil
}

// TODO(Monkeyanator) validate the versions
func parseVersion(version string) (Version, error) {
	return Version(version), nil
}
