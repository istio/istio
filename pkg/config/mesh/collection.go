package mesh

import (
	"os"

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
	}, krt.WithName("MeshConfig_File"), krt.WithStop(stop))
}

func NewCollection(primary *krt.Singleton[string], secondary *krt.Singleton[string], stop <-chan struct{}) krt.Singleton[MeshConfigResource] {
	return krt.NewSingleton[MeshConfigResource](
		func(ctx krt.HandlerContext) *MeshConfigResource {
			meshCfg := DefaultMeshConfig()
			if secondary != nil {
				s := krt.FetchOne(ctx, (*secondary).AsCollection())
				n, err := ApplyMeshConfig(*s, meshCfg)
				if err != nil {
					log.Error(err)
					// TODO!!!
					return nil
				}
				meshCfg = n
			}
			if primary != nil {
				s := krt.FetchOne(ctx, (*primary).AsCollection())
				n, err := ApplyMeshConfig(*s, meshCfg)
				if err != nil {
					log.Error(err)
					// TODO!!!
					return nil
				}
				meshCfg = n
			}
			return &MeshConfigResource{meshCfg}
		}, krt.WithName("MeshConfig"), krt.WithStop(stop),
	)
}
