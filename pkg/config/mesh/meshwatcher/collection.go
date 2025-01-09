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

func NewCollection(primary *MeshConfigSource, secondary *MeshConfigSource, stop <-chan struct{}) krt.Singleton[MeshConfigResource] {
	return krt.NewSingleton[MeshConfigResource](
		func(ctx krt.HandlerContext) *MeshConfigResource {
			meshCfg := mesh.DefaultMeshConfig()

			log.Errorf("howardjohn: computing collection!")
			for _, attempt := range []*MeshConfigSource{secondary, primary} {
				if attempt == nil {
					// Source is not specified, skip it
					continue
				}
				s := krt.FetchOne(ctx, (*attempt).AsCollection())
				log.Errorf("howardjohn: got source %v", s)
				if s == nil {
					// Source specified but not giving us any data
					continue
				}
				log.Errorf("howardjohn: merge in config %v", *s)
				n, err := mesh.ApplyMeshConfig(*s, meshCfg)
				if err != nil {
					log.Warnf("invalid mesh config, using last known state: %v", err)
					ctx.DiscardResult()
					return nil
				}
				meshCfg = n
			}
			log.Errorf("howardjohn: final %v", meshCfg.IngressClass)
			return &MeshConfigResource{meshCfg}
		}, krt.WithName("MeshConfig"), krt.WithStop(stop), krt.WithDebugging(krt.GlobalDebugHandler),
	)
}

func NewNetworksCollection(primary *MeshConfigSource, secondary *MeshConfigSource, stop <-chan struct{}) krt.Singleton[MeshNetworksResource] {
	return krt.NewSingleton[MeshNetworksResource](
		func(ctx krt.HandlerContext) *MeshNetworksResource {
			for _, attempt := range []*MeshConfigSource{secondary, primary} {
				if attempt == nil {
					continue
				}
				if s := krt.FetchOne(ctx, (*attempt).AsCollection()); s != nil {
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
