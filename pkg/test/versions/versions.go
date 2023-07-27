package versions

import (
	"fmt"
	"github.com/Masterminds/semver/v3"
	"istio.io/istio/pkg/lazy"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/image"
)

type versionInfo struct {
	Latest *semver.Version
	// Previous is not always Latest-1, so store it
	Previous *semver.Version
}

func get() versionInfo {
	v, err := versions.Get()
	if err != nil {
		panic(fmt.Sprintf("failed to load versions: %v", err))
	}
	return v
}

// Latest returns the current version
func Latest() string {
	return translate(get().Latest)
}

// Previous returns the N'th previous version. For example, if latest is 1.2, this Previous(1) would return 1.1.
// Exception: if 1.1 has not yet been published, 1.0 would be returned instead.
func Previous(n int) string {
	return translate(delta(get().Previous, -n))
}

func translate(sv *semver.Version) string {
	v := sv.String()
	switch v {
		case "1.17.0":
		// 1.17.0 was broken, workaround
			return "1.17.1"
		default:
			return v
	}
}
const imageToCheck = "gcr.io/istio-release/pilot"
var versions = lazy.New[versionInfo](func() (versionInfo, error) {
	versionFromFile, err := env.ReadVersion()
	if err != nil {
		return versionInfo{}, err
	}

	v, err := semver.NewVersion(versionFromFile)
	if err != nil {
		return versionInfo{}, err
	}

	previousVersion := delta(v, -1)

	// If the previous version is not published yet, use the latest one
	if exists, err := image.Exists(imageToCheck + ":" + previousVersion.String()); err != nil {
		return versionInfo{}, err
	} else if !exists {
		previousVersion = delta(v, -2)
	}

	return versionInfo{
		Latest:   v,
		Previous: previousVersion,
	}, nil
})

func delta(v *semver.Version, delta int) *semver.Version {
	return semver.New(v.Major(), uint64(int(v.Minor())+delta), v.Patch(), v.Prerelease(), v.Metadata())
}