# Multiple CUSTOM Authorization Providers - Integration Tests

This directory contains integration tests for the multiple CUSTOM authorization providers feature added in PR #58082.

## Background

Prior to this change, Istio enforced a restriction that only one CUSTOM authorization provider could be configured per workload. This made it impossible to use different authentication schemes for different API paths on the same service (e.g., OAuth for `/api/*`, LDAP for `/admin/*`, API keys for `/webhooks/*`).

These integration tests validate that multiple providers can now coexist and work correctly.

## Test Files

### Test Implementation

**`authz_multiple_providers_test.go`** - Main test file containing:

- `TestAuthz_MultipleCustomProviders_NonOverlapping` - Tests two providers with different path prefixes
- `TestAuthz_MultipleCustomProviders_Overlapping` - Tests what happens when provider rules overlap
- `TestAuthz_MultipleCustomProviders_ProviderOrdering` - Documents provider evaluation order
- `TestAuthz_MultipleCustomProviders_FilterChainVerification` - Provides manual verification steps
- `TestAuthz_MultipleCustomProviders_DryRunMixed` - Placeholder for future dry-run testing

### Policy Templates

**`testdata/authz/multiple-providers-non-overlapping.yaml.tmpl`** - Two providers handling separate paths:
- Provider1 handles `/api/*` paths
- Provider2 handles `/admin/*` paths

**`testdata/authz/multiple-providers-overlapping.yaml.tmpl`** - Two providers with overlapping rules:
- Provider1 handles `/api/*`
- Provider2 handles `/api/admin/*`

## Running the Tests

Run all multiple provider tests:

```bash
go test -tags=integ ./tests/integration/security \
  -test.run=TestAuthz_MultipleCustomProviders \
  -istio.test.hub=<hub> \
  -istio.test.tag=<tag>
```

Run a specific test:

```bash
go test -tags=integ ./tests/integration/security \
  -test.run=TestAuthz_MultipleCustomProviders_Overlapping \
  -istio.test.hub=<hub> \
  -istio.test.tag=<tag> \
  -test.v
```

## What the Tests Validate

### Non-Overlapping Paths Test

This is the primary use case: different providers handling different paths on the same workload.

The test verifies:
- Provider1 correctly allows/denies requests to `/api/*`
- Provider2 correctly allows/denies requests to `/admin/*`
- Providers don't interfere with each other
- Paths not covered by any provider remain accessible

### Overlapping Paths Test

This test answers the critical question: what happens when a request matches multiple providers?

Key findings:
- When multiple providers match, ALL must allow for the request to succeed
- If ANY provider denies, the request is blocked
- This is AND logic, not OR logic
- Providers are processed in alphabetical order by name

For example, if a request to `/api/admin/users` matches both `provider-oauth` and `provider-ldap`:
- Both allow → request succeeds
- Either denies → request fails with 403

### Provider Ordering Test

Documents that providers are processed alphabetically by provider name. This ordering is deterministic and implemented in `builder.go:306-307`:

```go
uniqueProviders := maps.Keys(rule.providerRules)
sort.Strings(uniqueProviders)
```

### Filter Chain Verification Test

Provides commands to manually inspect the generated Envoy configuration. After applying policies with multiple providers, you can verify the filter chain structure:

```bash
POD=$(kubectl get pods -n <namespace> -l app=<service> -o jsonpath='{.items[0].metadata.name}')

# View filter chain
istioctl proxy-config listeners $POD -n <namespace> --port 8080 -o json | \
  jq '.[] | .filterChains[0].filters[] | .name'

# View metadata matchers
istioctl proxy-config listeners $POD -n <namespace> -o json | \
  jq '.[] | .. | .filterEnabledMetadata? | select(. != null)'
```

Expected: one RBAC and ext_authz filter pair per provider, ordered alphabetically.

## Implementation Notes

### How Multiple Providers Work

The implementation generates separate filter pairs for each provider:

Rules are grouped by provider in `builder.go:188-190`:

```go
providerRules := map[string]*rbacpb.RBAC{}
providerShadowRules := map[string]*rbacpb.RBAC{}
```

Each provider gets its own RBAC and ext_authz filters (`builder.go:309-350`):

```text
[RBAC-provider1] → [ExtAuthz-provider1] → [RBAC-provider2] → [ExtAuthz-provider2]
```

Provider-specific metadata matching ensures each filter only triggers for its own policies (`extauthz.go:370-392`):

```go
prefix := fmt.Sprintf("%s-%s", extAuthzMatchPrefix, provider)
```

Policy names include the provider identifier (`builder.go:444`):

```text
istio-ext-authz-{provider}-ns[namespace]-policy[name]-rule[index]
```

### Evaluation Semantics

When a request matches policies from multiple providers:

- Envoy evaluates each provider's RBAC filter in alphabetical order
- Each matching provider's ext_authz service is called
- The request succeeds only if ALL providers allow
- If ANY provider denies, the request is blocked immediately

This is implemented through the filter chain structure - each provider operates as a separate enforcement point.

## Known Limitations

- **Provider failure test is skipped**: Would require infrastructure to create a misconfigured provider
- **Filter chain verification is manual**: Could be automated by parsing istioctl output
- **No performance benchmarks**: Latency impact of N providers is not measured

## Troubleshooting

### Test Skipped - Not Enough Providers

The tests require at least 2 ext_authz providers. Check that both `authzServer` and `localAuthzServer` are configured in `main_test.go`.

### No Suitable Target Workloads Found

The target matching logic requires workloads that are supported by both selected providers. This typically means workloads with both HTTP and GRPC ports.

### Request Not Blocked When Expected

If overlapping provider tests fail, check:
1. Both providers are actually configured (check istioctl proxy-config)
2. Policy names include provider identifiers (should see `istio-ext-authz-{provider}-*` in logs)
3. Metadata matchers are provider-specific (check filter chain config)

## Future Enhancements

- Automate filter chain verification by parsing config dumps
- Add provider failure isolation test with broken provider setup
- Add performance benchmarks for N-provider configurations
- Add TCP protocol coverage (currently focused on HTTP/GRPC)
- Add dry-run + enforce mixed mode test for the same provider

## References

- PR: #58082
- Issues: #57933, #55142, #34041
- Design considerations documented in `pilot/pkg/security/authz/builder/builder.go`
