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

package handler

import (
	"context"
	"time"

	"go.opencensus.io/stats"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/protobuf/yaml/dynamic"
	"istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/runtime/monitoring"
	"istio.io/istio/mixer/pkg/runtime/safecall"
	"istio.io/pkg/log"
	"istio.io/pkg/pool"
)

const (
	defaultRetryDuration = 1 * time.Second
	defaultRetryChecks   = 10
)

// Table contains a set of instantiated and configured adapter handlers.
type Table struct {
	entries map[string]Entry

	monitoringCtx context.Context

	strayWorkersRetryDuration time.Duration
	strayWorkersCheckRetries  int
}

// Entry in the handler table.
type Entry struct {
	// Name of the Handler
	Name string

	// Handler is the initialized Handler object.
	Handler adapter.Handler

	// AdapterName that was used to create this Entry.
	AdapterName string

	// Signature of the configuration used to create this entry.
	Signature signature

	// env refers to the adapter.Env passed to the handler.
	env env
}

// NewTable returns a new table, based on the given config snapshot. The table will re-use existing handlers as much as
// possible from the old table.
func NewTable(old *Table, snapshot *config.Snapshot, gp *pool.GoroutinePool, namespaces []string) *Table {
	// Find all handlers, as referenced by instances, and associate to handlers.
	instancesByHandler := config.GetInstancesGroupedByHandlers(snapshot)
	instancesByHandlerDynamic := config.GetInstancesGroupedByHandlersDynamic(snapshot)

	t := &Table{
		entries:                   make(map[string]Entry, len(instancesByHandler)+len(instancesByHandlerDynamic)),
		monitoringCtx:             context.Background(),
		strayWorkersCheckRetries:  defaultRetryChecks,
		strayWorkersRetryDuration: defaultRetryDuration,
	}

	var newAdapters, reusedAdapters, buildErrs int64
	for handler, instances := range instancesByHandler {
		n, r, be := createEntry(old, t, handler, instances, snapshot.ID,
			func(handler hndlr, instances interface{}) (h adapter.Handler, e env, err error) {
				e = NewEnv(snapshot.ID, handler.GetName(), gp, namespaces).(env)
				h, err = config.BuildHandler(handler.(*config.HandlerStatic), instances.([]*config.InstanceStatic),
					e, snapshot.Templates)
				return h, e, err
			})

		newAdapters += n
		reusedAdapters += r
		buildErrs += be
	}

	for handler, instances := range instancesByHandlerDynamic {
		n, r, be := createEntry(old, t, handler, instances, snapshot.ID,
			func(_ hndlr, _ interface{}) (h adapter.Handler, e env, err error) {
				e = NewEnv(snapshot.ID, handler.GetName(), gp, namespaces).(env)
				tmplCfg := make([]*dynamic.TemplateConfig, 0, len(instances))
				for _, inst := range instances {
					tmplCfg = append(tmplCfg, &dynamic.TemplateConfig{
						Name:         inst.Name,
						TemplateName: inst.Template.Name,
						FileDescSet:  inst.Template.FileDescSet,
						Variety:      inst.Template.Variety,
					})
				}
				h, err = dynamic.BuildHandler(handler.GetName(), handler.Connection,
					handler.Adapter.SessionBased, handler.AdapterConfig, tmplCfg, false)
				return h, e, err
			})

		newAdapters += n
		reusedAdapters += r
		buildErrs += be
	}

	stats.Record(t.monitoringCtx,
		monitoring.NewHandlersTotal.M(newAdapters),
		monitoring.ReusedHandlersTotal.M(reusedAdapters),
		monitoring.BuildFailuresTotal.M(buildErrs),
	)
	return t
}

type buildHandlerFn func(handler hndlr, instances interface{}) (h adapter.Handler, env env, err error)

func createEntry(old *Table, t *Table, handler hndlr, instances interface{}, snapshotID int64, buildHandler buildHandlerFn) (added, reused, errors int64) {

	sig := calculateSignature(handler, instances)

	currentEntry, found := old.entries[handler.GetName()]
	if found && currentEntry.Signature.equals(sig) {
		// reuse the Handler
		t.entries[handler.GetName()] = currentEntry
		reused++
		return
	}

	instantiatedHandler, e, err := buildHandler(handler, instances)

	if err != nil {
		errors++
		log.Errorf(
			"Unable to initialize adapter: snapshot='%d', handler='%s', adapter='%s', err='%s'.\n"+
				"Please remove the handler or fix the configuration.",
			snapshotID, handler.GetName(), handler.AdapterName(), err.Error())
		return
	}

	added++

	t.entries[handler.GetName()] = Entry{
		Name:        handler.GetName(),
		Handler:     instantiatedHandler,
		AdapterName: handler.AdapterName(),
		Signature:   sig,
		env:         e,
	}

	return added, reused, errors
}

// Cleanup the old table by selectively closing handlers that are not used in the given table.
// The cleanup method is called on the "old" table, and the "current" table (that is based on the new config)
// is passed as a parameter. The Cleanup method selectively closes all adapters that are not used by the current
// table. This method will use perf counters on current will be used, instead of the perf counters on t.
// This ensures that appropriate config id dimension is used when reporting metrics.
func (t *Table) Cleanup(current *Table) {
	var toCleanup []Entry

	for name, oldEntry := range t.entries {
		if currentEntry, found := current.entries[name]; found && currentEntry.Signature.equals(oldEntry.Signature) {
			// this entry is still in use. Skip it.
			continue
		}

		// schedule for cleanup
		toCleanup = append(toCleanup, oldEntry)
	}

	var closed, closeErrs int64
	for _, entry := range toCleanup {
		log.Debugf("Closing adapter %s/%v", entry.Name, entry.Handler)
		var err error
		panicErr := safecall.Execute("handler.Close", func() {
			err = entry.Handler.Close()
		})

		if panicErr != nil {
			err = panicErr
		}

		go func(adapterEnv env, name string) {
			strayWorkersFound := adapterEnv.hasStrayWorkers()
			for i := 0; i < t.strayWorkersCheckRetries && strayWorkersFound; i++ {
				adapterEnv.Logger().Debugf("Found stray workers for adapter: %s; will check again in %s", name, t.strayWorkersRetryDuration)
				time.Sleep(t.strayWorkersRetryDuration)
				strayWorkersFound = adapterEnv.hasStrayWorkers()
			}

			if strayWorkersFound {
				adapterEnv.reportStrayWorkers()
			} else {
				adapterEnv.Logger().Infof("adapter closed all scheduled daemons and workers")
			}
		}(entry.env, entry.Name)

		if err != nil {
			closeErrs++
			log.Warnf("Error closing adapter: %s/%v: '%v'", entry.Name, entry.Handler, err)
		} else {
			closed++
		}
	}

	ctx := context.Background()
	if t != nil && t.monitoringCtx != nil {
		ctx = t.monitoringCtx
	}
	stats.Record(ctx,
		monitoring.ClosedHandlersTotal.M(closed),
		monitoring.CloseFailuresTotal.M(closeErrs),
	)
}

// Get returns the entry for a Handler with the given name, if it exists.
func (t *Table) Get(handlerName string) (Entry, bool) {
	e, found := t.entries[handlerName]
	if !found {
		return Entry{}, false
	}

	return e, true
}

var emptyTable = &Table{}

// Empty returns an empty table instance.
func Empty() *Table {
	return emptyTable
}
