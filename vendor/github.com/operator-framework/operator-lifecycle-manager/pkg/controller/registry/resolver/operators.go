package resolver

import (
	"fmt"
	"strings"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	opregistry "github.com/operator-framework/operator-registry/pkg/registry"
)

type CatalogKey struct {
	Name      string
	Namespace string
}

func (k *CatalogKey) String() string {
	return fmt.Sprintf("%s/%s", k.Name, k.Namespace)
}

type APISet map[opregistry.APIKey]struct{}

func EmptyAPISet() APISet {
	return map[opregistry.APIKey]struct{}{}
}

func (s APISet) PopAPIKey() *opregistry.APIKey {
	for a := range s {
		api := &opregistry.APIKey{
			Group:   a.Group,
			Version: a.Version,
			Kind:    a.Kind,
			Plural:  a.Plural,
		}
		delete(s, a)
		return api
	}
	return nil
}

type APIOwnerSet map[opregistry.APIKey]OperatorSurface

func EmptyAPIOwnerSet() APIOwnerSet {
	return map[opregistry.APIKey]OperatorSurface{}
}

type OperatorSet map[string]OperatorSurface

func EmptyOperatorSet() OperatorSet {
	return map[string]OperatorSurface{}
}

type APIMultiOwnerSet map[opregistry.APIKey]OperatorSet

func EmptyAPIMultiOwnerSet() APIMultiOwnerSet {
	return map[opregistry.APIKey]OperatorSet{}
}

func (s APIMultiOwnerSet) PopAPIKey() *opregistry.APIKey {
	for a := range s {
		api := &opregistry.APIKey{
			Group:   a.Group,
			Version: a.Version,
			Kind:    a.Kind,
			Plural:  a.Plural,
		}
		delete(s, a)
		return api
	}
	return nil
}

func (s APIMultiOwnerSet) PopAPIRequirers() OperatorSet {
	requirers := EmptyOperatorSet()
	for a := range s {
		for key, op := range s[a] {
			requirers[key] = op
		}
		delete(s, a)
		return requirers
	}
	return nil
}

type OperatorSourceInfo struct {
	Package string
	Channel string
	Catalog CatalogKey
}

func (i *OperatorSourceInfo) String() string {
	return fmt.Sprintf("%s/%s in %s/%s", i.Package, i.Channel, i.Catalog.Name, i.Catalog.Namespace)
}

var ExistingOperator = OperatorSourceInfo{"", "", CatalogKey{"", ""}}

// OperatorSurface describes the API surfaces provided and required by an Operator.
type OperatorSurface interface {
	ProvidedAPIs() APISet
	RequiredAPIs() APISet
	Identifier() string
	Replaces() string
	SourceInfo() *OperatorSourceInfo
	Bundle() *opregistry.Bundle
}

type Operator struct {
	name         string
	replaces     string
	providedAPIs APISet
	requiredAPIs APISet
	bundle       *opregistry.Bundle
	sourceInfo   *OperatorSourceInfo
}

var _ OperatorSurface = &Operator{}

func NewOperatorFromBundle(bundle *opregistry.Bundle, sourceKey CatalogKey) (*Operator, error) {
	csv, err := bundle.ClusterServiceVersion()
	if err != nil {
		return nil, err
	}
	providedAPIs, err := bundle.ProvidedAPIs()
	if err != nil {
		return nil, err
	}
	requiredAPIs, err := bundle.RequiredAPIs()
	if err != nil {
		return nil, err
	}
	return &Operator{
		name:         csv.GetName(),
		replaces:     csv.Spec.Replaces,
		providedAPIs: providedAPIs,
		requiredAPIs: requiredAPIs,
		bundle:       bundle,
		sourceInfo: &OperatorSourceInfo{
			Package: bundle.Package,
			Channel: bundle.Channel,
			Catalog: sourceKey,
		},
	}, nil
}

func NewOperatorFromCSV(csv *v1alpha1.ClusterServiceVersion) (*Operator, error) {
	providedAPIs := EmptyAPISet()
	for _, crdDef := range csv.Spec.CustomResourceDefinitions.Owned {
		parts := strings.SplitN(crdDef.Name, ".", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("error parsing crd name: %s", crdDef.Name)
		}
		providedAPIs[opregistry.APIKey{Plural: parts[0], Group: parts[1], Version: crdDef.Version, Kind: crdDef.Kind}] = struct{}{}
	}
	for _, api := range csv.Spec.APIServiceDefinitions.Owned {
		providedAPIs[opregistry.APIKey{Group: api.Group, Version: api.Version, Kind: api.Kind, Plural: api.Name}] = struct{}{}
	}

	requiredAPIs := EmptyAPISet()
	for _, crdDef := range csv.Spec.CustomResourceDefinitions.Required {
		parts := strings.SplitN(crdDef.Name, ".", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("error parsing crd name: %s", crdDef.Name)
		}
		requiredAPIs[opregistry.APIKey{Plural: parts[0], Group: parts[1], Version: crdDef.Version, Kind: crdDef.Kind}] = struct{}{}
	}
	for _, api := range csv.Spec.APIServiceDefinitions.Required {
		requiredAPIs[opregistry.APIKey{Group: api.Group, Version: api.Version, Kind: api.Kind, Plural: api.Name}] = struct{}{}
	}

	return &Operator{
		name:         csv.GetName(),
		replaces:     csv.Spec.Replaces,
		providedAPIs: providedAPIs,
		requiredAPIs: requiredAPIs,
		sourceInfo:   &ExistingOperator,
	}, nil
}

func (o *Operator) ProvidedAPIs() APISet {
	return o.providedAPIs
}

func (o *Operator) RequiredAPIs() APISet {
	return o.requiredAPIs
}

func (o *Operator) Identifier() string {
	return o.name
}

func (o *Operator) Replaces() string {
	return o.replaces
}

func (o *Operator) Package() string {
	return o.bundle.Package
}

func (o *Operator) SourceInfo() *OperatorSourceInfo {
	return o.sourceInfo
}

func (o *Operator) Bundle() *opregistry.Bundle {
	return o.bundle
}
