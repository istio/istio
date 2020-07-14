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

// Package routing implements a routing table for resolving incoming requests to handlers. The table data model
// is structured for efficient use by the runtime code during actual dispatch. At a high-level, the structure
// of table is as follows:
//
// Table:               map[variety]varietyTable
// varietyTable:        map[namespace]NamespaceTable
// NamespaceTable:      list(Destination)
// Destination:         unique(handler&template) + list(InstanceGroup)
// InstanceGroup:       condition + list(InstanceBuilders) + list(OutputMappers)
//
// The call into table.GetDestinations performs a lookup on the first map by the variety (i.e. quota, check,
// report, apa etc.), followed by a lookup on the second map for the namespace, and a NamespaceTable struct
// is returned.
//
// The returned NamespaceTable holds all the handlers that should be dispatched to, along with conditions and
// builders for the instances. These include handlers that were defined for the namespace of the request, as
// well as the handlers from the default namespace. If there were no explicit rules in the request's namespace,
// then only the handlers from the default namespace is applied. Similarly, if the request is for the default
// namespace, then only the handlers from the default namespace is applied.
//
// Beneath the namespace layer, the same handler can appear multiple times in this list for each template that
// is supported by the handler. This helps caller to ensure that each dispatch to the handler will use a unique
// template.
//
// The client code is expected to work as follows:
// - Call GetDestinations(variety, namespace) to get a NamespaceTable.
// - Go through the list of entries in the NamespaceTable.
// - For each entry begin a dispatch session to the associated handler.
// - Go through the InstanceGroup
// - For each InstanceGroup, check the condition and see if the inputs/outputs apply.
// - If applies, then call InstanceBuilders to create instances
// - Depending on the variety, either aggregate all instances in the group, and send them all at once, or
//   dispatch for every instance individually to the adapter.
//
package routing

import (
	"context"
	"fmt"
	"strings"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/runtime/handler"
	"istio.io/istio/mixer/pkg/runtime/lang"
	"istio.io/istio/mixer/pkg/runtime/monitoring"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/attribute"
	"istio.io/pkg/log"
)

// builder keeps the ephemeral state while the routing table is built.
type builder struct {
	// table that is being built.
	table                  *Table
	handlers               *handler.Table
	defaultConfigNamespace string

	// compilers for snapshot attribute manifest
	compilers map[lang.LanguageRuntime]lang.Compiler

	// id counter for assigning ids to various items in the hierarchy. These reference into the debug
	// information.
	nextIDCounter uint32

	// Ephemeral data that can also be used as debugging info.

	// match condition sets by the input set id.
	matchesByID map[uint32]string

	// instanceName set of builders by the input set.
	instanceNamesByID map[uint32][]string

	// InstanceBuilderFns by instance name.
	builders map[string]template.InstanceBuilderFn

	// OutputMapperFns by instance name.
	mappers map[string]template.OutputMapperFn

	// compiled.Expressions by canonicalized rule match clauses
	expressions map[string]compiled.Expression

	// snapshot attribute manifest
	attributes attribute.AttributeDescriptorFinder
}

// BuildTable builds and returns a routing table. If debugInfo is set, the returned table will have debugging information
// attached, which will show up in String() call.
func BuildTable(
	handlers *handler.Table,
	config *config.Snapshot,
	defaultConfigNamespace string,
	debugInfo bool) *Table {

	b := &builder{

		table: &Table{
			id:      config.ID,
			entries: make(map[tpb.TemplateVariety]*varietyTable, 4),
		},

		handlers:               handlers,
		defaultConfigNamespace: defaultConfigNamespace,
		compilers:              make(map[lang.LanguageRuntime]lang.Compiler),
		nextIDCounter:          1,

		matchesByID:       make(map[uint32]string, len(config.Rules)),
		instanceNamesByID: make(map[uint32][]string, len(config.InstancesStatic)),

		builders:    make(map[string]template.InstanceBuilderFn, len(config.InstancesStatic)),
		mappers:     make(map[string]template.OutputMapperFn, len(config.InstancesStatic)),
		expressions: make(map[string]compiled.Expression, len(config.Rules)),

		attributes: config.Attributes,
	}

	b.build(config)

	if debugInfo {
		b.table.debugInfo = &tableDebugInfo{
			matchesByID:       b.matchesByID,
			instanceNamesByID: b.instanceNamesByID,
		}
	}

	return b.table
}

func (b *builder) nextID() uint32 {
	id := b.nextIDCounter
	b.nextIDCounter++
	return id
}

func (b *builder) build(snapshot *config.Snapshot) {

	var unsatActions, matchErrs int64

	for _, rule := range snapshot.Rules {

		// Create a compiled expression for the rule condition first.
		condition, err := b.getConditionExpression(rule)
		if err != nil {
			log.Warnf("Unable to compile match condition expression: '%v', rule='%s', expression='%s'",
				err, rule.Name, rule.Match)
			matchErrs++
			// Skip the rule
			continue
		}

		// For each action, find unique instances to use, and add entries to the map.
		for i, action := range rule.ActionsStatic {

			// Find the matching handler.
			handlerName := action.Handler.Name
			entry, found := b.handlers.Get(handlerName)
			if !found {
				// This can happen if we cannot initialize a handler, even if the config itself self-consistent.
				log.Warnf("Unable to find a handler for action. rule[action]='%s[%d]', handler='%s'",
					rule.Name, i, handlerName)

				unsatActions++
				// Skip the rule
				continue
			}

			for _, instance := range action.Instances {
				// get the instance mapper and builder for this instance. Mapper is used by APA instances
				// to map the instance result back to attributes.
				builder, mapper, err := b.getBuilderAndMapper(instance)
				if err != nil {
					log.Warnf("Unable to create builder/mapper for instance: instance='%s', err='%v'", instance.Name, err)
					continue
				}

				b.add(rule.Namespace, buildTemplateInfo(instance.Template), entry, condition, builder, mapper,
					entry.Name, instance.Name, rule.Match, action.Name)
			}
		}

		// process dynamic actions
		for i, action := range rule.ActionsDynamic {

			// Find the matching handler.
			handlerName := action.Handler.Name
			entry, found := b.handlers.Get(handlerName)
			if !found {
				// This can happen if we cannot initialize a handler, even if the config itself self-consistent.
				log.Warnf("Unable to find a handler for action. rule[action]='%s[%d]', handler='%s'",
					rule.Name, i, handlerName)

				unsatActions++
				// Skip the rule
				continue
			}

			for _, instance := range action.Instances {
				// get the instance mapper and builder for this instance. Mapper is used by APA instances
				// to map the instance result back to attributes.
				builder, mapper := b.getBuilderAndMapperDynamic(instance)

				b.add(rule.Namespace, b.templateInfo(instance.Template), entry, condition, builder, mapper,
					entry.Name, instance.Name, rule.Match, action.Name)
			}
		}

		// process rule operations
		if len(rule.RequestHeaderOperations) > 0 || len(rule.ResponseHeaderOperations) > 0 {
			compiler := b.buildRuleCompiler(snapshot.Attributes, rule)
			operations, err := b.buildRuleOperations(compiler, rule)
			if err != nil {
				log.Warnf("Unable to compile rule operations: %q, rule=%q", err, rule.Name)
				continue
			}

			b.addRuleOperations(rule.Namespace, condition, operations)
		}
	}

	// Capture the default namespace rule set and flatten all default namespace rule into other namespace tables for
	// faster processing.
	for _, vTable := range b.table.entries {
		defaultSet, found := vTable.entries[b.defaultConfigNamespace]
		if !found {
			log.Warnf("No destination sets found for the default namespace '%s'.", b.defaultConfigNamespace)
			defaultSet = emptyDestinations
		}
		// Set the default rule set for the variety.
		vTable.defaultSet = defaultSet

		if defaultSet.Count() != 0 {
			// Prefix all namespace destinations with the destinations from the default namespace.
			for namespace, set := range vTable.entries {
				if namespace == b.defaultConfigNamespace {
					// Skip the default namespace itself
					continue
				}

				set.entries = append(defaultSet.entries, set.entries...)
			}
		}
	}

	for variety, vTable := range b.table.entries {
		totalDests := 0
		for _, nsTable := range vTable.entries {
			totalDests += nsTable.Count()
		}
		ctx := context.Background()
		var err error
		if ctx, err = tag.New(ctx, tag.Insert(monitoring.VarietyTag, variety.String())); err != nil {
			log.Errorf("error establishing monitoring context for variety type: %v", err)
		}
		stats.Record(ctx, monitoring.DestinationsPerVarietyTotal.M(int64(totalDests)))
	}

	stats.Record(snapshot.MonitoringContext,
		monitoring.UnsatisfiedActionHandlers.M(unsatActions),
		monitoring.MatchErrors.M(matchErrs),
	)
}

func (b *builder) compiler(mode lang.LanguageRuntime) lang.Compiler {
	if out, ok := b.compilers[mode]; ok {
		return out
	}

	out := lang.NewBuilder(b.attributes, mode)
	b.compilers[mode] = out
	return out
}

// get or create a builder and a mapper for the given instance. The mapper is created only if the template
// is an attribute generator.
func (b *builder) getBuilderAndMapper(
	instance *config.InstanceStatic) (template.InstanceBuilderFn, template.OutputMapperFn, error) {
	var err error

	t := instance.Template

	builder := b.builders[instance.Name]
	exprb := b.compiler(instance.Language)
	if builder == nil {
		if builder, err = t.CreateInstanceBuilder(instance.Name, instance.Params, exprb); err != nil {
			return nil, nil, err
		}
		b.builders[instance.Name] = builder
	}

	var mapper template.OutputMapperFn
	if t.Variety == tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR {
		mapper = b.mappers[instance.Name]
		if mapper == nil {
			var expressions map[string]compiled.Expression
			if expressions, err = t.CreateOutputExpressions(instance.Params, b.attributes, exprb); err != nil {
				return nil, nil, err
			}
			mapper = template.NewOutputMapperFn(expressions)
		}

		b.mappers[instance.Name] = mapper
	}

	return builder, mapper, nil
}

// get or create a compiled.Expression for the rule's match clause, if necessary.
func (b *builder) getConditionExpression(rule *config.Rule) (compiled.Expression, error) {
	text := strings.TrimSpace(rule.Match)

	if text == "" {
		return nil, nil
	}

	// Minor optimization for a simple case.
	if text == "true" {
		return nil, nil
	}

	expression := b.expressions[text]
	if expression == nil {
		var err error
		var t descriptor.ValueType
		if expression, t, err = b.compiler(rule.Language).Compile(text); err != nil {
			return nil, err
		}
		if t != descriptor.BOOL {
			return nil, fmt.Errorf("expression does not return a boolean: '%s'", text)
		}

		b.expressions[text] = expression
	}

	return expression, nil
}

func (b *builder) add(
	namespace string,
	t *TemplateInfo,
	entry handler.Entry,
	condition compiled.Expression,
	builder template.InstanceBuilderFn,
	mapper template.OutputMapperFn,
	handlerName string,
	instanceName string,
	matchText string,
	actionName string) {

	// CHECK_WITH_OUTPUT is grouped into CHECK variety table
	variety := t.Variety
	if variety == tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT {
		variety = tpb.TEMPLATE_VARIETY_CHECK
	}

	// Find or create the variety entry.
	byVariety, found := b.table.entries[variety]
	if !found {
		byVariety = &varietyTable{
			entries: make(map[string]*NamespaceTable),
		}
		b.table.entries[variety] = byVariety
	}

	// Find or create the namespace entry.
	byNamespace, found := byVariety.entries[namespace]
	if !found {
		byNamespace = &NamespaceTable{
			entries: []*Destination{},
		}
		byVariety.entries[namespace] = byNamespace
	}

	// Find or create the handler&template entry.
	var byHandler *Destination
	for _, d := range byNamespace.Entries() {
		if d.HandlerName == entry.Name && d.Template.Name == t.Name {
			byHandler = d
			break
		}
	}

	if byHandler == nil {
		byHandler = &Destination{
			id:             b.nextID(),
			Handler:        entry.Handler,
			FriendlyName:   fmt.Sprintf("%s:%s(%s)", t.Name, handlerName, entry.AdapterName),
			HandlerName:    handlerName,
			AdapterName:    entry.AdapterName,
			Template:       t,
			InstanceGroups: []*InstanceGroup{},
		}
		byNamespace.entries = append(byNamespace.entries, byHandler)
	}

	// TODO(Issue #2690): We should dedupe instances that are being dispatched to a particular handler.

	// Find or create the input set.
	var instanceGroup *InstanceGroup
	for _, set := range byHandler.InstanceGroups {
		// Try to find an input set to place the entry by comparing the compiled expression and resource type.
		// This doesn't flatten across all actions, but only for actions coming from the same rule. We can
		// flatten based on the expression text as well.
		if set.Condition == condition {
			instanceGroup = set
			break
		}
	}

	if instanceGroup == nil {
		instanceGroup = &InstanceGroup{
			id:        b.nextID(),
			Condition: condition,
			Builders:  []NamedBuilder{},
			Mappers:   []template.OutputMapperFn{},
		}
		byHandler.InstanceGroups = append(byHandler.InstanceGroups, instanceGroup)

		if matchText != "" {
			b.matchesByID[instanceGroup.id] = matchText
		}

		// Create a slot in the debug info for storing the instance names for this input-set.
		instanceNames, found := b.instanceNamesByID[instanceGroup.id]
		if !found {
			instanceNames = make([]string, 0, 1)
		}
		b.instanceNamesByID[instanceGroup.id] = instanceNames
	}

	// Append the builder & mapper.
	instanceGroup.Builders = append(instanceGroup.Builders, NamedBuilder{InstanceShortName: config.ExtractShortName(instanceName), Builder: builder,
		ActionName: actionName})

	if mapper != nil {
		instanceGroup.Mappers = append(instanceGroup.Mappers, mapper)
	}

	// Recalculate the maximum number of instances that can be created.
	byHandler.recalculateMaxInstances()

	// record the instance name for this id.
	instanceNames := b.instanceNamesByID[instanceGroup.id]
	instanceNames = append(instanceNames, instanceName)
	b.instanceNamesByID[instanceGroup.id] = instanceNames
}

// templateInfo build method needed dispatch this template
func (b *builder) templateInfo(tmpl *config.Template) *TemplateInfo {
	ti := &TemplateInfo{
		Name:    tmpl.Name,
		Variety: tmpl.Variety,
	}

	// dynamic dispatch to APA adapters
	ti.DispatchGenAttrs = func(ctx context.Context, handler adapter.Handler, instance interface{},
		attrs attribute.Bag, mapper template.OutputMapperFn) (*attribute.MutableBag, error) {
		var h adapter.RemoteGenerateAttributesHandler
		var ok bool
		var encodedInstance *adapter.EncodedInstance

		if h, ok = handler.(adapter.RemoteGenerateAttributesHandler); !ok {
			return nil, fmt.Errorf("internal: handler of incorrect type. got %T, want: RemoteGenerateAttributes", handler)
		}

		if encodedInstance, ok = instance.(*adapter.EncodedInstance); !ok {
			return nil, fmt.Errorf("internal: instance of incorrect type. got %T, want: []byte", instance)
		}

		valuesBag := attribute.GetMutableBag(attrs)
		defer valuesBag.Done()
		if err := h.HandleRemoteGenAttrs(ctx, encodedInstance, valuesBag); err != nil {
			return nil, fmt.Errorf("internal: failed to make an RPC to an APA: %v", err)
		}

		out, err := mapper(valuesBag)
		if err != nil {
			return nil, fmt.Errorf("internal: failed to map attributes from the output: %v", err)
		}

		return out, nil
	}

	// Make a call to check
	ti.DispatchCheck = func(ctx context.Context, handler adapter.Handler, instance interface{},
		out *attribute.MutableBag, outPrefix string) (adapter.CheckResult, error) {
		var h adapter.RemoteCheckHandler
		var ok bool
		var encodedInstance *adapter.EncodedInstance

		if h, ok = handler.(adapter.RemoteCheckHandler); !ok {
			return adapter.CheckResult{}, fmt.Errorf("internal: handler of incorrect type. got %T, want: RemoteCheckHandler", handler)
		}

		if encodedInstance, ok = instance.(*adapter.EncodedInstance); !ok {
			return adapter.CheckResult{}, fmt.Errorf("internal: instance of incorrect type. got %T, want: []byte", instance)
		}

		cr, err := h.HandleRemoteCheck(ctx, encodedInstance, out, outPrefix)
		if err != nil {
			return adapter.CheckResult{}, err
		}

		return *cr, nil
	}

	ti.DispatchReport = func(ctx context.Context, handler adapter.Handler, instances []interface{}) error {
		var h adapter.RemoteReportHandler
		var ok bool
		var encodedInstance *adapter.EncodedInstance

		if h, ok = handler.(adapter.RemoteReportHandler); !ok {
			return fmt.Errorf("internal: handler of incorrect type. got %T, want: RemoteReportHandler", handler)
		}

		encodedInstances := make([]*adapter.EncodedInstance, len(instances))

		for i := range instances {
			instance := instances[i]
			if encodedInstance, ok = instance.(*adapter.EncodedInstance); !ok {
				return fmt.Errorf("internal: instance of incorrect type. got %T, want: []byte", instance)
			}
			encodedInstances[i] = encodedInstance
		}
		return h.HandleRemoteReport(ctx, encodedInstances)
	}

	ti.DispatchQuota = func(ctx context.Context, handler adapter.Handler, instance interface{}, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
		var h adapter.RemoteQuotaHandler
		var ok bool
		var encodedInstance *adapter.EncodedInstance

		if h, ok = handler.(adapter.RemoteQuotaHandler); !ok {
			return adapter.QuotaResult{}, fmt.Errorf("internal: handler of incorrect type. got %T, want: RemoteQuotaHandler", handler)
		}

		if encodedInstance, ok = instance.(*adapter.EncodedInstance); !ok {
			return adapter.QuotaResult{}, fmt.Errorf("internal: instance of incorrect type. got %T, want: []byte", instance)
		}

		qr, err := h.HandleRemoteQuota(ctx, encodedInstance, &args)
		if err != nil {
			return adapter.QuotaResult{}, err
		}
		return *qr, nil
	}
	return ti
}

const defaultInstanceSize = 128

// get or create a builder and a mapper for the given instance. The mapper is created only if the template
// is an attribute generator. At present this function never returns an error.
func (b *builder) getBuilderAndMapperDynamic(
	instance *config.InstanceDynamic) (template.InstanceBuilderFn, template.OutputMapperFn) {
	var instBuilder template.InstanceBuilderFn = func(attrs attribute.Bag) (interface{}, error) {
		var err error
		ba := make([]byte, 0, defaultInstanceSize)
		// The encoder produces
		if ba, err = instance.Encoder.Encode(attrs, ba); err != nil {
			return nil, err
		}

		return &adapter.EncodedInstance{
			Name: instance.Name,
			Data: ba,
		}, nil
	}

	var mapper template.OutputMapperFn
	if instance.Template.Variety == tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR {
		mapper = b.mappers[instance.Name]
		if mapper == nil {
			chained := attribute.NewChainedFinder(b.attributes, instance.Template.AttributeManifest)
			expb := lang.NewBuilder(chained, instance.Language)

			expressions := make(map[string]compiled.Expression)
			for attrName, outExpr := range instance.AttributeBindings {
				attrInfo := b.attributes.GetAttribute(attrName)
				if attrInfo == nil {
					log.Warnf("attribute not found when mapping outputs: attr=%q, expr=%q", attrName, outExpr)
					continue
				}
				expr, expType, err := expb.Compile(outExpr)
				if err != nil {
					log.Warnf("attribute expression compilation failure: expr=%q, %v", outExpr, err)
					continue
				}
				if attrInfo.ValueType != expType {
					log.Warnf("attribute type mismatch: attr=%q, attrType='%v', expr=%q, exprType='%v'", attrName, attrInfo.ValueType, outExpr, expType)
					continue
				}
				expressions[attrName] = expr
			}
			mapper = template.NewOutputMapperFn(expressions)
		}

		b.mappers[instance.Name] = mapper
	}
	return instBuilder, mapper
}

// buildRuleCompiler constructs an expression compiler over an extended attribute vocabulary
// with template output attributes prefixed by the action names added to the global attribute manifests.
func (b *builder) buildRuleCompiler(parent attribute.AttributeDescriptorFinder, rule *config.Rule) lang.Compiler {
	// templates include the output template attributes in their manifests
	attributeDescriptor := make(map[string]*descriptor.AttributeManifest_AttributeInfo)

	for _, action := range rule.ActionsStatic {
		if len(action.Instances) == 0 {
			continue
		}

		// assuming identical templates for all instances
		template := action.Instances[0].Template
		if template.Variety != tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT {
			continue
		}

		for _, manifest := range template.AttributeManifests {
			for attrName, attrInfo := range manifest.Attributes {
				attributeDescriptor[action.Name+".output."+attrName] = attrInfo
			}
		}
	}

	for _, action := range rule.ActionsDynamic {
		if len(action.Instances) == 0 {
			continue
		}

		// assuming identical templates for all instances
		template := action.Instances[0].Template
		if template.Variety != tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT {
			continue
		}

		// dynamic template output attributes start with "output."
		for attrName, attrInfo := range template.AttributeManifest {
			attributeDescriptor[action.Name+"."+attrName] = attrInfo
		}
	}

	return lang.NewBuilder(attribute.NewChainedFinder(parent, attributeDescriptor), rule.Language)
}

// buildRuleOperations creates an intermediate symbolic form for the route directive header operations
func (b *builder) buildRuleOperations(compiler lang.Compiler, rule *config.Rule) ([]*HeaderOperation, error) {
	reqOps, err := b.compileRuleOperationTemplates(rule.Name, compiler, RequestHeaderOperation, rule.RequestHeaderOperations)
	if err != nil {
		return nil, err
	}

	respOps, err := b.compileRuleOperationTemplates(rule.Name, compiler, ResponseHeaderOperation, rule.ResponseHeaderOperations)
	if err != nil {
		return nil, err
	}

	return append(reqOps, respOps...), nil
}

func (b *builder) compileRuleOperationTemplates(
	location string,
	compiler lang.Compiler,
	typ HeaderOperationType,
	ops []*descriptor.Rule_HeaderOperationTemplate) ([]*HeaderOperation, error) {

	out := make([]*HeaderOperation, 0, len(ops))

	for _, op := range ops {
		// ignore values if operation is header removal
		if op.Operation == descriptor.REMOVE {
			out = append(out, &HeaderOperation{
				Type:       typ,
				HeaderName: op.Name,
				Operation:  op.Operation,
			})
			continue
		}

		for _, value := range op.Values {
			ve, vt, verr := compiler.Compile(value)
			if verr != nil {
				return nil, fmt.Errorf("unable to compile header operation value expression: %q in %q, expression=%q",
					verr, location, value)
			}
			if vt != descriptor.STRING {
				return nil, fmt.Errorf("header operation value expression is not of string type: expression=%q in %q",
					value, location)
			}
			out = append(out, &HeaderOperation{
				Type:        typ,
				HeaderName:  op.Name,
				HeaderValue: ve,
				Operation:   op.Operation,
			})
		}
	}

	return out, nil
}

// addRuleOperations appends operation expressions to the CHECK entry in the routing table
// should be called after b.add() to ensure table initialization for the namespace
func (b *builder) addRuleOperations(
	namespace string,
	condition compiled.Expression,
	operations []*HeaderOperation) {

	// ensure struct population for rules with routeDirectives and no actions
	if b.table.entries == nil {
		b.table.entries = map[tpb.TemplateVariety]*varietyTable{}
	}
	if b.table.entries[tpb.TEMPLATE_VARIETY_CHECK] == nil {
		b.table.entries[tpb.TEMPLATE_VARIETY_CHECK] = &varietyTable{}
	}
	if b.table.entries[tpb.TEMPLATE_VARIETY_CHECK].entries == nil {
		b.table.entries[tpb.TEMPLATE_VARIETY_CHECK].entries = make(map[string]*NamespaceTable)
	}

	byNamespace, found := b.table.entries[tpb.TEMPLATE_VARIETY_CHECK].entries[namespace]
	if !found {
		byNamespace = &NamespaceTable{
			entries:    []*Destination{},
			directives: []*DirectiveGroup{},
		}
		b.table.entries[tpb.TEMPLATE_VARIETY_CHECK].entries[namespace] = byNamespace
	}

	var group *DirectiveGroup
	for _, set := range byNamespace.directives {
		if set.Condition == condition {
			group = set
			break
		}
	}
	if group == nil {
		group = &DirectiveGroup{
			Condition: condition,
		}
		byNamespace.directives = append(byNamespace.directives, group)
	}
	group.Operations = append(group.Operations, operations...)
}
