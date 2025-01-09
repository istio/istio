package meshwatcher

import (
	"os"
	"path"

	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/files"
	"istio.io/istio/pkg/log"
)

type MeshConfigSource = krt.Singleton[string]

func NewFileSource(fileWatcher filewatcher.FileWatcher, filename string, stop <-chan struct{}) (MeshConfigSource, error) {
	return files.NewSingleton[string](fileWatcher, filename, stop, func(filename string) (string, error) {
		b, err := os.ReadFile(filename)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}, krt.WithName("Mesh_File_"+path.Base(filename)), krt.WithStop(stop))
}

// NewCollection builds a new mesh config built by applying the provided sources.
// Sources are applied in order (example: default < sources[0] < sources[1]).
func NewCollection(stop <-chan struct{}, sources ...MeshConfigSource) krt.Singleton[MeshConfigResource] {
	return krt.NewSingleton[MeshConfigResource](
		func(ctx krt.HandlerContext) *MeshConfigResource {
			meshCfg := mesh.DefaultMeshConfig()

			log.Errorf("howardjohn: computing collection!")
			for _, attempt := range sources {
				s := krt.FetchOne(ctx, attempt.AsCollection())
				log.Errorf("howardjohn: got source %v", s)
				if s == nil {
					// Source specified but not giving us any data
					continue
				}
				log.Errorf("howardjohn: merge in config %v", *s)
				n, err := mesh.ApplyMeshConfig(*s, meshCfg)
				if err != nil {
					// For backwards compatibility, keep inconsistent behavior
					// TODO(https://github.com/istio/istio/issues/54615) align this.
					if len(sources) == 1 {
						log.Warnf("invalid mesh config, using last known state: %v", err)
						ctx.DiscardResult()
						return nil
					} else {
						log.Warnf("invalid mesh config, ignoring: %v", err)
						continue
					}
				}
				meshCfg = n
			}
			log.Errorf("howardjohn: final %v", meshCfg.IngressClass)
			return &MeshConfigResource{meshCfg}
		}, krt.WithName("MeshConfig"), krt.WithStop(stop), krt.WithDebugging(krt.GlobalDebugHandler),
	)
}

// NewNetworksCollection builds a new meshnetworks config built by applying the provided sources.
// Sources are applied in order (example: default < sources[0] < sources[1]).
func NewNetworksCollection(stop <-chan struct{}, sources ...MeshConfigSource) krt.Singleton[MeshNetworksResource] {
	return krt.NewSingleton[MeshNetworksResource](
		func(ctx krt.HandlerContext) *MeshNetworksResource {
			for _, attempt := range sources {
				if s := krt.FetchOne(ctx, attempt.AsCollection()); s != nil {
					n, err := mesh.ParseMeshNetworks(*s)
					if err != nil {
						log.Warnf("invalid mesh networks, using last known state: %v", err)
						ctx.DiscardResult()
						return nil
					}
					return &MeshNetworksResource{n}
				}
			}
			return &MeshNetworksResource{nil}
		}, krt.WithName("MeshNetworks"), krt.WithStop(stop),
	)
}
