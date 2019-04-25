package resolver

import (
	"github.com/pkg/errors"
)

// TODO: this should take a cancellable context for killing long resolution
// TODO: return a set of errors or warnings of unusual states to know about (we expect evolve to always succeed, because it can be a no-op)

// Evolvers modify a generation to a new state
type Evolver interface {
	Evolve(add map[OperatorSourceInfo]struct{}) error
}

type NamespaceGenerationEvolver struct {
	querier SourceQuerier
	gen     Generation
}

func NewNamespaceGenerationEvolver(querier SourceQuerier, gen Generation) Evolver {
	return &NamespaceGenerationEvolver{querier: querier, gen: gen}
}

// Evolve takes new requested operators, adds them to the generation, and attempts to resolve dependencies with querier
func (e *NamespaceGenerationEvolver) Evolve(add map[OperatorSourceInfo]struct{}) error {
	if err := e.querier.Queryable(); err != nil {
		return err
	}

	// check for updates to existing operators
	if err := e.checkForUpdates(); err != nil {
		return err
	}

	// fetch bundles for new operators (aren't yet tracked)
	if err := e.addNewOperators(add); err != nil {
		return err
	}

	// attempt to resolve any missing apis as a result expanding the generation of operators
	if err := e.queryForRequiredAPIs(); err != nil {
		return err
	}

	// for any remaining missing APIs, attempt to downgrade the operator that required them
	// this may contract the generation back to the original set!
	e.downgradeAPIs()
	return nil
}

func (e *NamespaceGenerationEvolver) checkForUpdates() error {
	for _, op := range e.gen.Operators() {
		// only check for updates if we have sourceinfo
		if op.SourceInfo() == &ExistingOperator {
			continue
		}

		bundle, key, err := e.querier.FindReplacement(op.Identifier(), op.SourceInfo().Package, op.SourceInfo().Channel, op.SourceInfo().Catalog)
		if err != nil || bundle == nil {
			continue
		}

		o, err := NewOperatorFromBundle(bundle, *key)
		if err != nil {
			return errors.Wrap(err, "error parsing bundle")
		}
		if err := e.gen.AddOperator(o); err != nil {
			if err != nil {
				return errors.Wrap(err, "error calculating generation changes due to new bundle")
			}
		}
		e.gen.RemoveOperator(op)
	}
	return nil
}

func (e *NamespaceGenerationEvolver) addNewOperators(add map[OperatorSourceInfo]struct{}) error {
	for s := range add {
		bundle, key, err := e.querier.FindPackage(s.Package, s.Channel, s.Catalog)

		if err != nil {
			// TODO: log or collect warnings
			return errors.Wrapf(err, "%s not found", s)
		}

		o, err := NewOperatorFromBundle(bundle, *key)
		if err != nil {
			return errors.Wrap(err, "error parsing bundle")
		}
		if err := e.gen.AddOperator(o); err != nil {
			if err != nil {
				return errors.Wrap(err, "error calculating generation changes due to new bundle")
			}
		}
	}
	return nil
}

func (e *NamespaceGenerationEvolver) queryForRequiredAPIs() error {
	e.gen.ResetUnchecked()

	for {
		api := e.gen.UncheckedAPIs().PopAPIKey()
		if api == nil {
			break
		}
		e.gen.MarkAPIChecked(*api)

		// attempt to find a bundle that provides that api
		if bundle, key, err := e.querier.FindProvider(*api); err == nil {
			// add a bundle that provides the api to the generation
			o, err := NewOperatorFromBundle(bundle, *key)
			if err != nil {
				return errors.Wrap(err, "error parsing bundle")
			}
			if err := e.gen.AddOperator(o); err != nil {
				return errors.Wrap(err, "error calculating generation changes due to new bundle")
			}
		}
	}
	return nil
}

func (e *NamespaceGenerationEvolver) downgradeAPIs() {
	e.gen.ResetUnchecked()
	for missingAPIs := e.gen.MissingAPIs(); len(missingAPIs) > 0; {
		requirers := missingAPIs.PopAPIRequirers()
		for _, op := range requirers {
			e.gen.RemoveOperator(op)
		}
	}
}
