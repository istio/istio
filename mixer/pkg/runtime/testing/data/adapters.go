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

package data

import (
	"context"
	"fmt"
	"time"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/mixer/pkg/adapter"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// BuildAdapters builds a standard set of testing adapters. The supplied settings is used to override behavior.
func BuildAdapters(l *Logger, settings ...FakeAdapterSettings) map[string]*adapter.Info {
	m := make(map[string]FakeAdapterSettings)
	for _, setting := range settings {
		m[setting.Name] = setting
	}

	var a = map[string]*adapter.Info{
		"acheck":       createFakeAdapter("acheck", m["acheck"], l, "tcheck", "thalt"),
		"acheckoutput": createFakeAdapter("acheckoutput", m["acheckoutput"], l, "tcheckoutput"),
		"areport":      createFakeAdapter("areport", m["areport"], l, "treport"),
		"aquota":       createFakeAdapter("aquota", m["aquota"], l, "tquota"),
		"apa":          createFakeAdapter("apa", m["apa"], l, "tapa"),
	}

	return a
}

func createFakeAdapter(name string, s FakeAdapterSettings, l *Logger, defaultTemplates ...string) *adapter.Info {
	templates := defaultTemplates
	if s.SupportedTemplates != nil {
		templates = s.SupportedTemplates
	}

	// A healthy adapter that implements the check interface. It's behavior is configurable.
	return &adapter.Info{
		Name:               name,
		DefaultConfig:      &types.Struct{},
		SupportedTemplates: templates,
		NewBuilder: func() adapter.HandlerBuilder {
			l.Write(name, "NewBuilder =>")
			if s.NilBuilder {
				l.WriteFormat(name, "NewBuilder <= nil")
				return nil
			}

			l.WriteFormat(name, "NewBuilder <=")
			return &FakeHandlerBuilder{
				settings: s,
				l:        l,
			}
		},
	}
}

// FakeEnv is a dummy implementation of adapter.Env
type FakeEnv struct {
}

// Logger is an implementation of adapter.Env.Logger.
func (f *FakeEnv) Logger() adapter.Logger { panic("should not be called") }

// ScheduleWork is an implementation of adapter.Env.ScheduleWork.
func (f *FakeEnv) ScheduleWork(fn adapter.WorkFunc) { panic("should not be called") }

// ScheduleDaemon is an implementation of adapter.Env.ScheduleDaemon.
func (f *FakeEnv) ScheduleDaemon(fn adapter.DaemonFunc) { panic("should not be called") }

func (f *FakeEnv) NewInformer(
	clientset kubernetes.Interface,
	objType runtime.Object,
	duration time.Duration,
	listerWatcher func(namespace string) cache.ListerWatcher,
	indexers cache.Indexers) cache.SharedIndexInformer {
	panic("This method should not be called within the current tests")
}

var _ adapter.Env = &FakeEnv{}

// FakeHandlerBuilder is a fake of HandlerBuilder.
type FakeHandlerBuilder struct {
	l        *Logger
	settings FakeAdapterSettings
}

// SetAdapterConfig is an implementation of HandlerBuilder.SetAdapterConfig.
func (f *FakeHandlerBuilder) SetAdapterConfig(cfg adapter.Config) {
	f.l.WriteFormat(f.settings.Name, "HandlerBuilder.SetAdapterConfig => '%v'", cfg)
	if f.settings.PanicAtSetAdapterConfig {
		f.l.Write(f.settings.Name, "HandlerBuilder.SetAdapterConfig <= (PANIC)")
		panic(f.settings.PanicData)
	}
	f.l.Write(f.settings.Name, "HandlerBuilder.SetAdapterConfig <=")
}

// Validate is an implementation of HandlerBuilder.Validate.
func (f *FakeHandlerBuilder) Validate() *adapter.ConfigErrors {
	f.l.Write(f.settings.Name, "HandlerBuilder.Validate =>")
	if f.settings.PanicAtValidate {
		f.l.Write(f.settings.Name, "HandlerBuilder.Validate <= (PANIC)")
		panic(f.settings.PanicData)
	}

	if f.settings.ErrorAtValidate {
		f.l.Write(f.settings.Name, "HandlerBuilder.Validate <= (ERROR)")
		errs := &adapter.ConfigErrors{}
		errs = errs.Append("field", fmt.Errorf("some validation error"))
		return errs
	}

	f.l.Write(f.settings.Name, "HandlerBuilder.Validate <= (SUCCESS)")
	return nil
}

// Build is an implementation of HandlerBuilder.Build.
func (f *FakeHandlerBuilder) Build(_ context.Context, env adapter.Env) (adapter.Handler, error) {
	f.l.Write(f.settings.Name, "HandlerBuilder.Build =>")
	if f.settings.PanicAtBuild {
		f.l.Write(f.settings.Name, "HandlerBuilder.Build <= (PANIC)")
		panic(f.settings.PanicData)
	}

	if f.settings.ErrorAtBuild {
		f.l.Write(f.settings.Name, "HandlerBuilder.Build <= (ERROR)")
		return nil, fmt.Errorf("this adapter is not available at the moment, please come back later")
	}

	if f.settings.NilHandlerAtBuild {
		f.l.Write(f.settings.Name, "HandlerBuilder.Build <= (nil)")
		return nil, nil
	}

	handler := &FakeHandler{
		settings:      f.settings,
		l:             f.l,
		closingDaemon: make(chan bool),
		closingWorker: make(chan bool),
		done:          make(chan bool),
	}
	handler.refreshTicker = time.NewTicker(1 * time.Millisecond)
	if f.settings.SpawnDaemon {
		env.ScheduleDaemon(func() {
			for {
				select {
				case <-handler.refreshTicker.C:
					// do nothing
				case <-handler.closingDaemon:
					return
				}
			}
		})
	}

	if f.settings.SpawnWorker {
		env.ScheduleWork(func() {
			for {
				select {
				case <-handler.refreshTicker.C:
					// do nothing..
				case <-handler.closingWorker:
					return
				}
			}
		})
	}

	f.l.Write(f.settings.Name, "HandlerBuilder.Build <= (SUCCESS)")
	return handler, nil
}

var _ adapter.HandlerBuilder = &FakeHandlerBuilder{}

// FakeHandler is a fake implementation of adapter.Handler.
type FakeHandler struct {
	settings      FakeAdapterSettings
	l             *Logger
	refreshTicker *time.Ticker
	closingDaemon chan bool
	closingWorker chan bool
	done          chan bool
}

// Close is an implementation of adapter.Handler.Close.
func (f *FakeHandler) Close() error {
	f.l.Write(f.settings.Name, "Handler.Close =>")

	if f.settings.PanicAtHandlerClose {
		f.l.Write(f.settings.Name, "Handler.Close <= (PANIC)")
		panic(f.settings.PanicData)
	}

	if f.settings.ErrorAtHandlerClose {
		f.l.Write(f.settings.Name, "Handler.Close <= (ERROR)")
		return fmt.Errorf("error on close")
	}

	if f.settings.CloseGoRoutines {
		f.closingDaemon <- true
		f.closingWorker <- true
		close(f.closingDaemon)
		close(f.closingWorker)
		f.refreshTicker.Stop()
	}

	f.l.Write(f.settings.Name, "Handler.Close <= (SUCCESS)")
	return nil
}

var _ adapter.Handler = &FakeHandler{}

// FakeAdapterSettings describes the behavior of a fake adapter.
type FakeAdapterSettings struct { // nolint: maligned
	Name                    string
	PanicAtSetAdapterConfig bool
	PanicData               interface{}
	PanicAtValidate         bool
	ErrorAtValidate         bool
	ErrorAtBuild            bool
	PanicAtBuild            bool
	NilBuilder              bool
	NilHandlerAtBuild       bool
	ErrorAtHandlerClose     bool
	PanicAtHandlerClose     bool
	SpawnDaemon             bool
	SpawnWorker             bool
	CloseGoRoutines         bool

	SupportedTemplates []string
}
