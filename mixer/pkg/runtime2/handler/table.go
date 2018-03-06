// Copyright 2018 Istio Authors
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
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/pkg/log"
)

// Table contains a set of instantiated and configured adapter handlers.
type Table struct {
	entries map[string]Entry

	counters tableCounters
}

// Entry in the handler table.
type Entry struct {
	// Name of the Handler
	Name string

	// Handler is the initialized Handler object.
	Handler adapter.Handler

	// Adapter that was used to create this Entry.
	Adapter *adapter.Info

	// Signature of the configuration used to create this entry.
	Signature signature

	// env refers to the adapter.Env passed to the handler.
	env env
}

// NewTable returns a new table, based on the given config snapshot. The table will re-use existing handlers as much as
// possible from the old table.
func NewTable(old *Table, snapshot *config.Snapshot, gp *pool.GoroutinePool) *Table {
	var f *factory

	// Find all handlers, as referenced by instances, and associate to handlers.
	instancesByHandler := config.GetInstancesGroupedByHandlers(snapshot)

	t := &Table{
		entries:  make(map[string]Entry, len(instancesByHandler)),
		counters: newTableCounters(snapshot.ID),
	}

	for handler, instances := range instancesByHandler {
		sig := calculateSignature(handler, instances)

		currentEntry, found := old.entries[handler.Name]
		if found && currentEntry.Signature.equals(sig) {
			// reuse the Handler
			t.entries[handler.Name] = currentEntry
			t.counters.reusedHandlers.Inc()
			continue
		}

		// instantiate the new Handler
		if f == nil {
			f = newFactory(snapshot)
		}

		e := newEnv(snapshot.ID, handler.Name, gp)
		instantiatedHandler, err := f.build(handler, instances, e)

		if err != nil {
			t.counters.buildFailure.Inc()

			log.Errorf(
				"Unable to initialize adapter: snapshot='%d', handler='%s', adapter='%s', err='%s'.\n"+
					"Please remove the handler or fix the configuration.",
				snapshot.ID, handler.Name, handler.Adapter.Name, err.Error())

			continue
		}

		t.counters.newHandlers.Inc()

		t.entries[handler.Name] = Entry{
			Name:      handler.Name,
			Handler:   instantiatedHandler,
			Adapter:   handler.Adapter,
			Signature: sig,
			env:       e,
		}
	}

	return t
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

	for _, entry := range toCleanup {
		log.Debugf("Closing adapter %s/%v", entry.Name, entry.Handler)
		t.counters.closedHandlers.Inc()
		var err error
		panicErr := safeCall("handler.Close", func() {
			err = entry.Handler.Close()
		})

		if panicErr != nil {
			err = panicErr
		}

		if reportErr := entry.env.reportStrayWorkers(); reportErr != nil {
			log.Warnf("unable to report if there are any stray go routines scheduled by the adapter '%s': %v",
				entry.Name, reportErr)
		}

		if err != nil {
			t.counters.closeFailure.Inc()
			log.Warnf("Error closing adapter: %s/%v: '%v'", entry.Name, entry.Handler, err)
		}
	}
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
