# Analyzers

The purpose of analyzers is to examine Istio configuration for potential problems that should be surfaced back to the user. An analyzer takes as input a Context object that contains methods to inspect the configuration snapshot being analyzed, as well as methods for reporting any issues discovered.

## Writing Analyzers

### 1. Create the code

Analyzers need to implement the Analyzer interface ( in the `galley/pkg/config/analysis` package). They should be created under the analyzers subdirectory, and given their own appropriate subpackage.

An annotated example:

```go
package virtualservice

// <imports here>

type GatewayAnalyzer struct{}

// Compile-time check that this Analyzer correctly implements the interface
var _ analysis.Analyzer = &GatewayAnalyzer{}

// Metadata implements Analyzer
func (s *GatewayAnalyzer) Metadata() analysis.Metadata {
    return analysis.Metadata{
        // Each analyzer should have a unique name. Use <top-level-pkg>.<struct type>
        Name: "virtualservice.GatewayAnalyzer",
        // Each analyzer should have a short, one line description of what they
        // do. This description is shown when --list-analyzers is called via
        // the command line.
        Description: "Checks that VirtualService resources reference Gateways that exist",
        // Each analyzer should register the collections that it needs to use as input.
        Inputs: collection.Names{
            collections.IstioNetworkingV1Alpha3Gateways.Name(),
            collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
        },
    }
}

// Analyze implements Analyzer
func (s *GatewayAnalyzer) Analyze(c analysis.Context) {
    // The context object has several functions that let you access the configuration resources
    // in the current snapshot. The available collections, and how they map to k8s resources,
    // are defined in galley/pkg/config/schema/metadata.yaml
    // Available resources are listed under the "localAnalysis" snapshot in that file.
    c.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
        s.analyzeVirtualService(r, c)
        return true
    })
}

func (s *GatewayAnalyzer) analyzeVirtualService(r *resource.Instance, c analysis.Context) {
    // The actual resource entry, represented as a protobuf message, can be obtained via
    // the Item property of resource.Instance. It will need to be cast to the appropriate type.
    //
    // Since the resource.Instance also contains important metadata not included in the protobuf
    // message (such as the resource namespace/name) it's often useful to not do this casting
    // too early.
    vs := r.Item.(*v1alpha3.VirtualService)

    // The resource name includes the namespace, if one exists. It should generally be safe to
    // assume that the namespace is not blank, except for cluster-scoped resources.
    for i, gwName := range vs.Gateways {
        if !c.Exists(collections.IstioNetworkingV1Alpha3Gateways, resource.NewName(r.Metadata.FullName.Namespace, gwName)) {
            // Messages are defined in galley/pkg/config/analysis/msg/messages.yaml
            // From there, code is generated for each message type, including a constructor function
            // that you can use to create a new validation message of each type.
            msg := msg.NewReferencedResourceNotFound(r, "gateway", gwName)

            // Field map contains the path of the field as the key, and its line number as the value was stored in the resource.
            //
            // From the util package, find the correct path template of the field that needs to be analyzed, and
            // by giving the required parameters, the exact error line number can be found for the message for final displaying.
            //
            // If the path template does not exist, you can add the template in util/find_errorline_utils.go for future use.
            // If this exact line feature is not applied, or the message does not have a specific field like SchemaValidationError,
            // then the starting line number of the resource will be displayed instead.
            if line, ok := util.ErrorLine(r, fmt.Sprintf(util.VSGateway, i)); ok {
                msg.Line = line
            }

            // Messages are reported via the passed-in context object.
            c.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), msg)
        }
    }
}
```

### 2. Add the Analyzer to All()

In order to be run, analyzers need to be registered in the [analyzers.All()](https://github.com/istio/istio/blob/master/galley/pkg/config/analysis/analyzers/all.go) function. Add your analyzer as an entry there.

### 3. Create new message types

If your analyzer requires any new message types (meaning a unique template and error code), you will need to do the following:

1. Add an entry to [galley/pkg/config/analysis/msg/messages.yaml](https://github.com/istio/istio/blob/master/galley/pkg/config/analysis/msg/messages.yaml) e.g.

    ```yaml
      - name: "SandwichNotFound"
        code: IST0199
        level: Error
        description: "This resource requires a sandwich to function."
        template: "This resource requires a fresh %s on %s sandwich in order to run."
        args:
          - name: fillings
            type: string
          - name: bread
            type: string
    ```

1. Run `BUILD_WITH_CONTAINER=1 make gen`:

1. Use the new type in your analyzer

    ```go
    msg := msg.NewSandwichNotFound(resourceEntry, "ham", "rye")
    ```

Also note:

* Messages can have different levels (Error, Warning, Info).
* The code range 0000-0100 is reserved for internal and/or future use.
* Please keep entries in `messages.yaml` ordered by code.

### 4. Add path templates

If your analyzer requires to display the exact error line number, but the path template is not available, you should
add the template in [galley/pkg/config/analysis/analyzers/util/find_errorline_utils.go](https://github.com/istio/istio/blob/master/galley/pkg/config/analysis/analyzers/util/find_errorline_utils.go)

e.g for the GatewayAnalyzer used as an example above, you would add something like this to `find_errorline_utils.go`:

```go
    // Path for VirtualService gateway.
    // Required parameters: gateway index.
    VSGateway = "{.spec.gateways[%d]}"
```

### 5. Adding unit tests

For each new analyzer, you should add an entry to the test grid in
[analyzers_test.go](https://github.com/istio/istio/blob/master/galley/pkg/config/analysis/analyzers/analyzers_test.go).
If you want to add additional unit testing beyond this, you can, but analyzers are required to be covered in this test grid.

e.g. for the GatewayAnalyzer used as an example above, you would add something like this to `analyzers_test.go`:

```go
    {
        // Unique name for this test case
        name: "virtualServiceGateways",
        // List of input YAML files to load as resources before running analysis
        inputFiles: []string{
            "testdata/virtualservice_gateways.yaml",
        },
        // A single specific analyzer to run
        analyzer: &virtualservice.GatewayAnalyzer{},
        // List of expected validation messages, as (messageType, <kind> <name>[.<namespace>]) tuples
        expected: []message{
            {msg.ReferencedResourceNotFound, "VirtualService httpbin-bogus"},
        },
    },
```

`virtualservice_gateways.yaml`:

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: httpbin-gateway
spec:
  selector:
    istio: ingressgateway
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: httpbin
spec:
  hosts:
  - "*"
  gateways:
  - httpbin-gateway # Expected: no validation error since this gateway exists
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: httpbin-bogus
spec:
  hosts:
  - "*"
  gateways:
  - httpbin-gateway-bogus # Expected: validation error since this gateway does not exist
```

You should include both positive and negative test cases in the input YAML: we want to verify both that bad entries
generate messages and that good entries do not.

Since the analysis is tightly scoped and the YAML isn't checked for completeness, it's OK to partially specify the
resources in YAML to keep the test cases as simple and legible as possible.

Note that this test framework will also verify that the resources requested in testing match the resources listed as
inputs in the analyzer metadata. This should help you find any unused inputs and/or missing test cases.

### 6. Testing via istioctl

You can use `istioctl analyze` to run all analyzers, including your new one. e.g.

```sh
make istioctl && $GOPATH/out/linux_amd64/release/istioctl analyze
```

### 7. Write a user-facing documentation page

Each analysis message needs to be documented for customers. This is done by introducing a markdown file for
each message in the [istio.io](https://github.com/istio/istio.io) repo in the [content/en/docs/reference/config/analysis](https://github.com/istio/istio.io/tree/master/content/en/docs/reference/config/analysis) directory. You create
a subdirectory with the code of the error message, and add a `index.md` file that contains the
full description of the problem with potential remediation steps, examples, etc. See the existing
files in that directory for examples of how this is done.

## FAQ

### What if I need a resource not available as a collection?

Please open an issue (directed at the "Configuration" product area) or visit the
[\#config channel on Slack](https://istio.slack.com/messages/C7KSV4AHJ) to discuss it.
