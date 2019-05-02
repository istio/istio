//go:generate counterfeiter -o fakes/fake_registry_client.go ../../../../vendor/github.com/operator-framework/operator-registry/pkg/client/client.go Interface
//go:generate counterfeiter -o ../../../fakes/fake_resolver.go . Resolver
package resolver

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	v1alpha1listers "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/listers/operators/v1alpha1"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/operatorlister"
)

var timeNow = func() metav1.Time { return metav1.NewTime(time.Now().UTC()) }

type Resolver interface {
	ResolveSteps(namespace string, sourceQuerier SourceQuerier) ([]*v1alpha1.Step, []*v1alpha1.Subscription, error)
}

type OperatorsV1alpha1Resolver struct {
	subLister v1alpha1listers.SubscriptionLister
	csvLister v1alpha1listers.ClusterServiceVersionLister
}

var _ Resolver = &OperatorsV1alpha1Resolver{}

func NewOperatorsV1alpha1Resolver(lister operatorlister.OperatorLister) *OperatorsV1alpha1Resolver {
	return &OperatorsV1alpha1Resolver{
		subLister: lister.OperatorsV1alpha1().SubscriptionLister(),
		csvLister: lister.OperatorsV1alpha1().ClusterServiceVersionLister(),
	}
}

func (r *OperatorsV1alpha1Resolver) ResolveSteps(namespace string, sourceQuerier SourceQuerier) ([]*v1alpha1.Step, []*v1alpha1.Subscription, error) {
	if err := sourceQuerier.Queryable(); err != nil {
		return nil, nil, err
	}

	// create a generation - an representation of the current set of installed operators and their provided/required apis
	csvs, err := r.csvLister.ClusterServiceVersions(namespace).List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}
	subs, err := r.subLister.Subscriptions(namespace).List(labels.Everything())
	if err != nil {
		return nil, nil, err
	}

	gen, err := NewGenerationFromCluster(csvs, subs)
	if err != nil {
		return nil, nil, err
	}

	// create a map of operatorsourceinfo (subscription+catalogsource data) to the original subscriptions
	subMap := r.sourceInfoToSubscriptions(subs)
	// get a list of new operators to add to the generation
	add := r.sourceInfoForNewSubscriptions(namespace, subMap)

	// evolve a generation by resolving the set of subscriptions (in `add`) by querying with `source`
	// and taking the current generation (in `gen`) into account
	if err := NewNamespaceGenerationEvolver(sourceQuerier, gen).Evolve(add); err != nil {
		return nil, nil, err
	}

	// if there's no error, we were able to satsify all constraints in the subscription set, so we calculate what
	// changes to persist to the cluster and write them out as `steps`

	steps := []*v1alpha1.Step{}
	updatedSubs := []*v1alpha1.Subscription{}
	for name, op := range gen.Operators() {
		_, isAdded := add[*op.SourceInfo()]
		existingSubscription, subExists := subMap[*op.SourceInfo()]

		// subscription exists and is up to date
		if subExists && existingSubscription.Status.CurrentCSV == op.Identifier() && !isAdded {
			continue
		}

		// add steps for any new bundle
		if op.Bundle() != nil {
			bundleSteps, err := NewStepResourceFromBundle(op.Bundle(), namespace, op.SourceInfo().Catalog.Name, op.SourceInfo().Catalog.Namespace)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to turn bundle into steps")
			}
			for _, s := range bundleSteps {
				steps = append(steps, &v1alpha1.Step{
					Resolving: name,
					Resource:  s,
					Status:    v1alpha1.StepStatusUnknown,
				})
			}

			// add steps for subscriptions for bundles that were added through resolution
			if !subExists {
				subStep, err := NewSubscriptionStepResource(namespace, *op.SourceInfo())
				if err != nil {
					return nil, nil, err
				}
				steps = append(steps, &v1alpha1.Step{
					Resolving: name,
					Resource:  subStep,
					Status:    v1alpha1.StepStatusUnknown,
				})
			}
		}

		// update existing subscriptions status
		if subExists && existingSubscription.Status.CurrentCSV != op.Identifier() {
			existingSubscription.Status.CurrentCSV = op.Identifier()
			updatedSubs = append(updatedSubs, existingSubscription)
			continue
		}
	}

	return steps, updatedSubs, nil
}

func (r *OperatorsV1alpha1Resolver) sourceInfoForNewSubscriptions(namespace string, subs map[OperatorSourceInfo]*v1alpha1.Subscription) (add map[OperatorSourceInfo]struct{}) {
	add = make(map[OperatorSourceInfo]struct{})
	for key, sub := range subs {
		if sub.Status.CurrentCSV == "" {
			add[key] = struct{}{}
			continue
		}
		csv, err := r.csvLister.ClusterServiceVersions(namespace).Get(sub.Status.CurrentCSV)
		if csv == nil || errors.IsNotFound(err) {
			add[key] = struct{}{}
			continue
		}
	}
	return
}

func (r *OperatorsV1alpha1Resolver) sourceInfoToSubscriptions(subs []*v1alpha1.Subscription) (add map[OperatorSourceInfo]*v1alpha1.Subscription) {
	add = make(map[OperatorSourceInfo]*v1alpha1.Subscription)
	for _, s := range subs {
		add[OperatorSourceInfo{
			Package: s.Spec.Package,
			Channel: s.Spec.Channel,
			Catalog: CatalogKey{Name: s.Spec.CatalogSource, Namespace: s.Spec.CatalogSourceNamespace},
		}] = s.DeepCopy()
	}
	return
}
