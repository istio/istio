# Istio Mixer: Adapter Development Walkthrough

This document walks through step-by-step instructions to implement, test and plug a simple adapter into Mixer. For
complete details on the adapter life cycle, please refer to the [Adapter Developer's Guide](./adapters.md).

**Note**: To complete this walkthrough, it is optional to read the adapter developer's guide. However, to
create a real production quality adapter, it is highly recommended you read the guide to better understand
adapter lifecycle and various interfaces and objects that Mixer uses to interact with adapters.

In this walkthrough you're going to create a simple adapter that:

* Supports the [`metric`](../../template/metric/template.proto)  template which ships with Mixer.

* For every request, prints to a file the data it receives from Mixer at request time.

**It should take approximately ~30 minutes to finish this task**

* [Before you start](#before-you-start)
* [Step 1: Write basic adapter skeleton code](#step-1-write-basic-adapter-skeleton-code)
* [Step 2: Write adapter configuration](#step-2-write-adapter-configuration)
* [Step 3: Link adapter config with adapter code](#step-3-link-adapter-config-with-adapter-code)
* [Step 4: Write business logic into your adapter](#step-4-write-business-logic-into-your-adapter)
* [Step 5: Plug adapter into the Mixer](#step-5-plug-adapter-into-the-mixer)
* [Step 6: Write sample operator config](#step-6-write-sample-operator-config)
* [Step 7: Start Mixer and validate the adapter](#step-7-start-mixer-and-validate-the-adapter)
* [Step 8: Write test and validate your adapter (optional)](#step-8-write-test-and-validate-your-adapter-optional)
* [Step 9: Cleanup](#step-9-cleanup)
* [Step 10: Next](#step-10-next)

# Before you start

Download a local copy of the Mixer repo

```bash
git clone https://github.com/istio/istio
```

Install bazel (version 0.5.2 or higher) from [https://bazel.build/](https://bazel.build/) and add it to your PATH

Set the MIXER_REPO variable to the path where the mixer repository is on the local machine. Example `export MIXER_REPO=$GOPATH/src/istio.io/istio/mixer`

Successfully build the repo.

```bash
pushd $MIXER_REPO && bazel build ...
```

# Step 1: Write basic adapter skeleton code

Create the `mysampleadapter` directory and navigate to it.

```bash
cd $MIXER_REPO/adapter && mkdir mysampleadapter && cd mysampleadapter
```

Create the file named mysampleadapter.go with the following content

_It defines the adapter's `builder` and `handler` types
along with the interfaces required to support the 'metric' template. This code so far does not add any functionality for
printing details in a file. It is done in later steps._

```golang
package mysampleadapter

import (
  "context"

  "github.com/gogo/protobuf/types"
  "istio.io/istio/mixer/pkg/adapter"
  "istio.io/istio/mixer/template/metric"
)

type (
  builder struct {
  }
  handler struct {
  }
)

// ensure types implement the requisite interfaces
var _ metric.HandlerBuilder = &builder{}
var _ metric.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
  return &handler{}, nil
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) { return nil }

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
}

////////////////// Request-time Methods //////////////////////////
// metric.Handler#HandleMetric
func (h *handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {
  return nil
}

// adapter.Handler#Close
func (h *handler) Close() error { return nil }

////////////////// Bootstrap //////////////////////////
// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
  return adapter.Info{
     Name:        "mysampleadapter",
     Description: "Logs the metric calls into a file",
     SupportedTemplates: []string{
        metric.TemplateName,
     },
     NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
     DefaultConfig: &types.Empty{},
  }
}
```


Now, write a corresponding BUILD file. Create a file name BUILD with the following content.

```
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["mysampleadapter.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/adapter:go_default_library",
        "//template/metric:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)
```


Just to ensure everything is good, let's build the code

```bash
bazel build ...
```


The build output on the terminal should look like
```
INFO: Found 1 target...
Target //adapter/mysampleadapter:go_default_library up-to-date:
bazel-bin/adapter/mysampleadapter/~lib~/istio.io/istio/mixer/adapter/mysampleadapter
```

Now we have the basic skeleton of an adapter with empty implementation for interfaces for the 'metric' templates. Later steps
adds the core code for this adapter.

# Step 2: Write adapter configuration

Since this adapter just prints the data it receives from Mixer into a file, the adapter configuration will take the
path of that file as a configuration field.

Create the config proto file under the 'config' dir

```bash
mkdir config
```

Create a new config.proto file inside the config directory with the following content:


```proto
syntax = "proto3";

package adapter.mysampleadapter.config;

import "google/protobuf/duration.proto";
import "gogoproto/gogo.proto";

option go_package="config";

message Params {
    // Path of the file to save the information about runtime requests.
    string file_path = 1;
}
```


Create a BUILD file in the config directory with the following content:


Copy the following content to the config/BUILD file.

```
package(default_visibility = ["//visibility:public"])

load("@org_pubref_rules_protobuf//gogo:rules.bzl", "gogoslick_proto_library")

gogoslick_proto_library(
    name = "go_default_library",
    importmap = {
        "gogoproto/gogo.proto": "github.com/gogo/protobuf/gogoproto",
        "google/protobuf/duration.proto": "github.com/gogo/protobuf/types",
    },
    imports = [
        "external/com_github_gogo_protobuf",
        "external/com_github_google_protobuf/src",
    ],
    inputs = [
        "@com_github_gogo_protobuf//gogoproto:go_default_library_protos",
        "@com_github_google_protobuf//:well_known_protos",
    ],
    protos = [
        "config.proto",
    ],
    visibility = ["//adapter/mysampleadapter:__pkg__"],
    deps = [
        "@com_github_gogo_protobuf//gogoproto:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
    ],
)
```


Just to ensure everything is good, let's build the code

```bash
bazel build ...
```


The build output on the terminal should look like
```
INFO: Found 1 target...
Target //adapter/mysampleadapter:go_default_library up-to-date:
bazel-bin/adapter/mysampleadapter/~lib~/istio.io/istio/mixer/adapter/mysampleadapter
```
# Step 3: Link adapter config with adapter code.

Reference the config build target from the adapter's BUILD file. To do this edit the existing
adapter/mysampleadapter/BUILD file. Final adapter/mysampleadapter/BUILD file looks like below with bold text showing the
new added text.

<pre><code>
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["mysampleadapter.go"],
    visibility = ["//visibility:public"],
    deps = [
        <b>"//adapter/mysampleadapter/config:go_default_library",</b>
        "//pkg/adapter:go_default_library",
        "//template/metric:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)
</code></pre>


Modify the adapter code (`mysampleadapter.go`) to use the adapter-specific configuration
(defined in `mysampleadapter/config/config.proto`) to instantiate the file to write to. Also update the `GetInfo`
function to allow operators to pass the adapter-specific config and for the adapter to validate the operator provided
config. Copy the following code and the bold text shows the new added code.

<pre>
package mysampleadapter

import (
    // "github.com/gogo/protobuf/types"
	"context"
	<b>"os"</b>
	<b>"path/filepath"</b>

	</b>"istio.io/istio/mixer/adapter/mysampleadapter/config"</b>
	"istio.io/istio/mixer/pkg/adapter"
    "istio.io/istio/mixer/template/metric"
)

type (
	builder struct {
		<b>adpCfg *config.Params</b>
	}
	handler struct {
		<b>f *os.File</b>
	}
)

// ensure types implement the requisite interfaces
var _ metric.HandlerBuilder = &builder{}
var _ metric.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	<b>file, err := os.Create(b.adpCfg.FilePath)
	return &handler{f: file}, err</b>

}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	<b>b.adpCfg = cfg.(*config.Params)</b>
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	<b>// Check if the path is valid
	if _, err := filepath.Abs(b.adpCfg.FilePath); err != nil {
		ce = ce.Append("file_path", err)
	}
	return</b>
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
}

////////////////// Request-time Methods //////////////////////////
// metric.Handler#HandleMetric
func (h *handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {
	return nil
}

// adapter.Handler#Close
func (h *handler) Close() error {
	<b>return h.f.Close()</b>
}

////////////////// Bootstrap //////////////////////////
// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "mysampleadapter",
		Description: "Logs the metric calls into a file",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
		<b>DefaultConfig: &config.Params{},</b>
	}
}
</pre>


Just to ensure everything is good, let's build the code

```bash
bazel build ...
```


The build output on the terminal should look like
```
INFO: Found 1 target...
Target //adapter/mysampleadapter:go_default_library up-to-date:
bazel-bin/adapter/mysampleadapter/~lib~/istio.io/istio/mixer/adapter/mysampleadapter
```

# Step 4: Write business logic into your adapter.



Print Instance and associated Type information in the file configured via adapter config. This requires storing the
metric type information at configuration-time and using it at request-time. To add this functionality, update the file
mysampleadapter.go to look like the following. Note the bold text shows the newly added code. 

<pre>
package mysampleadapter

import (
	"context"

	// "github.com/gogo/protobuf/types"
	<b>"fmt"</b>
	"os"
	"path/filepath"
	config "istio.io/istio/mixer/adapter/mysampleadapter/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

type (
	builder struct {
		adpCfg      *config.Params
		<b>metricTypes map[string]*metric.Type</b>
	}
	handler struct {
		f           *os.File
		<b>metricTypes map[string]*metric.Type
		env         adapter.Env</b>
	}
)

// ensure types implement the requisite interfaces
var _ metric.HandlerBuilder = &builder{}
var _ metric.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	var err error
	var file *os.File
	file, err = os.Create(b.adpCfg.FilePath)
	<b>return &handler{f: file, metricTypes: b.metricTypes, env: env}, err</b>

}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adpCfg = cfg.(*config.Params)
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	// Check if the path is valid
	if _, err := filepath.Abs(b.adpCfg.FilePath); err != nil {
		ce = ce.Append("file_path", err)
	}
	return
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
	<b>b.metricTypes = types</b>
}

////////////////// Request-time Methods //////////////////////////
// metric.Handler#HandleMetric
func (h *handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {
	<b>for _, inst := range insts {
		if _, ok := h.metricTypes[inst.Name]; !ok {
			h.env.Logger().Errorf("Cannot find Type for instance %s", inst.Name)
			continue
		}
		h.f.WriteString(fmt.Sprintf(`HandleMetric invoke for :
		Instance Name  :'%s'
		Instance Value : %v,
		Type           : %v`, inst.Name, *inst, *h.metricTypes[inst.Name]))
	}
	return nil</b>
}

// adapter.Handler#Close
func (h *handler) Close() error {
	return h.f.Close()
}

////////////////// Bootstrap //////////////////////////
// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "mysampleadapter",
		Description: "Logs the metric calls into a file",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{},
	}
}
</pre>


Just to ensure everything is good, let's build the code

```bash
bazel build ...
```


The build output on the terminal should look like
```
INFO: Found 1 target...
Target //adapter/mysampleadapter:go_default_library up-to-date:
bazel-bin/adapter/mysampleadapter/~lib~/istio.io/istio/mixer/adapter/mysampleadapter
```

This concludes the implementation part of the adapter code. Next steps show how to plug an adapter into a build of Mixer
and to verify your code's behavior.

# Step 5: Plug adapter into the Mixer.

Update the $MIXER_REPO/adapter/BUILD file to add the new 'mysampleadapter' into the Mixer's adapter inventory.

Add the lines in **bold** to the existing file. The `inventory_library` build rule should look like the following

<pre>
inventory_library(
   name = "go_default_library",
   packages = {
       # list of all adapters
       # "friendlyName" : "go_import_path"
       "svcctrl": "istio.io/istio/mixer/adapter/svcctrl",
       ...
       <b>"mysampleadapter": "istio.io/istio/mixer/adapter/mysampleadapter",</b>
   },
   deps = [
       # list of all go_default_library rule for adapters.
       "//adapter/svcctrl:go_default_library",
       ...
       <b>"//adapter/mysampleadapter:go_default_library",</b>
   ],
)
</pre>


Now your adapter is plugged into Mixer and ready to receive data.

# Step 6: Write sample operator config

To see if your adapter works, we will need a sample operator configuration. So, let's write a simple operator
configuration that we will give to Mixer for it to dispatch data to your sample adapter. We will need instance, handler
and rule configuration to be passed to the Mixers configuration server. First we copy a sample attributes config that
configures Mixer with an attributes vocabulary. We can then use those attributes in the sample operator configuration.

Create a directory where we can put sample operator config

```bash
mkdir sampleoperatorconfig
```


Copy the sample attribute vocabulary config

```bash
cp $MIXER_REPO/testdata/config/attributes.yaml sampleoperatorconfig
```


Create a sample operator config file with name `config.yaml` inside the `sampleoperatorconfig` directory with the following content:

Add the following content to the file `sampleoperatorconfig/config.yaml.`

```yaml
# instance configuration for template 'metric'
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
 name: requestcount
 namespace: istio-system
spec:
 value: "1"
 dimensions:
   source: source.labels["app"] | "unknown"
   target: target.service | "unknown"
   service: target.labels["app"] | "unknown"
   method: request.path | "unknown"
   version: target.labels["version"] | "unknown"
   response_code: response.code | 200
 monitored_resource_type: '"UNSPECIFIED"'
---
# handler configuration for adapter 'metric'
apiVersion: "config.istio.io/v1alpha2"
kind: mysampleadapter
metadata:
 name: hndlrTest
 namespace: istio-system
spec:
 file_path: "out.txt"
---
# rule to dispatch to your handler
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
 name: mysamplerule
 namespace: istio-system
spec:
 match: "true"
 actions:
 - handler: hndlrTest.mysampleadapter
   instances:
   - requestcount.metric
```


# Step 7: Start Mixer and validate the adapter.

Start the mixer pointing it to the sample operator configuration

```bash
cd $MIXER_REPO && bazel build ... && bazel-bin/cmd/server/mixs server --configStore2URL=fs://$MIXER_REPO/adapter/mysampleadapter/sampleoperatorconfig --configStoreURL=fs://$MIXER_REPO
```

The terminal will have the following output and will be blocked waiting to serve requests

```
..
..
Starting self-monitoring on port 9093
Istio Mixer: version: 0.2.2-28-gc5112ac1 (build: 2017-09-23-c5112ac1, status: Modified)
Starting gRPC server on port 9091
```

Now let's call 'report' using mixer client. This step should cause the mixer server to call your sample adapter with
instance objects constructed using the operator configuration.

Start a new terminal window and set the MIXER_REPO variable to the path where the mixer repository is on the local
machine. Example ``export MIXER_REPO=$GOPATH/src/istio.io/istio/mixer``

In the new window call the following

```bash
cd $MIXER_REPO && bazel build ...
```


Invoke report

```bash
bazel-bin/cmd/client/mixc report -s="destination.service=svc.cluster.local"
```


Inspect the out.text file that your adapter would have printed. If you have followed the above steps, then the out.txt
should be in your directory `$MIXER_REPO`

```bash
tail $MIXER_REPO/out.txt
```

You should see something like:

<pre>
HandleMetric invoke for
       Instance Name  : requestcount.metric.istio-system
       Instance Value : {requestcount.metric.istio-system 1 map[response_code:200 service:unknown source:unknown target:unknown version:unknown method:unknown] UNSPECIFIED map[]}
       Type           : {INT64 map[response_code:INT64 service:STRING source:STRING target:STRING version:STRING method:STRING] map[]}
</pre>

You can even try passing other attributes to mixer server and inspect your out.txt file to see how the data passed to
the adapter changes. For example

```bash
bazel-bin/cmd/client/mixc report -s="destination.service=svc.cluster.local,target.service=mySrvc" -i="response.code=400" --stringmap_attributes="target.labels=app:dummyapp"
```

**If you have reached this far, congratulate yourself !!**. You have successfully created a Mixer adapter. You can
close (cltr + c) on your terminal that was running mixer server to shut it down.

# Step 8: Write test and validate your adapter (optional).

The above steps 7 (start mixer server and validate ..) were mainly to test your adapter code. You can achieve the same
thing by writing a simple test that uses the Mixer's 'testenv' package to start a inproc Mixer and make calls to it via
mixer client. For complete reference details on how to test adapter code check out [Test an adapter](./adapters.md#testing)

Add a test file for your adapter code

```bash
touch $MIXER_REPO/adapter/mysampleadapter/mysampleadapter_test.go
```

Add the following content to that file

```golang
package mysampleadapter

import (
	"io"
	"log"
	"testing"

	"golang.org/x/net/context"

	mixerapi "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/test/testenv"
	"istio.io/istio/mixer/template"
	"path/filepath"
)

func TestMySampleAdapter(t *testing.T) {
	operatorCnfg,err := filepath.Abs("sampleoperatorconfig")
	if err != nil {
		t.Fatalf("fail to get absolute path for sampleoperatorconfig: %v", err)
	}

	var args = testenv.Args{
		// Start Mixer server on a free port on loop back interface
		MixerServerAddr:               `127.0.0.1:0`,
		ConfigStoreURL:                `fs://` + operatorCnfg,
		ConfigStore2URL:               `fs://` + operatorCnfg,
		ConfigDefaultNamespace:        "istio-system",
		ConfigIdentityAttribute:       "destination.service",
		ConfigIdentityAttributeDomain: "svc.cluster.local",
	}

	env, err := testenv.NewEnv(&args, template.SupportedTmplInfo, []adapter.InfoFn{GetInfo})
	if err != nil {
		t.Fatalf("fail to create testenv: %v", err)
	}
	defer closeHelper(env)

	client, conn, err := env.CreateMixerClient()
	if err != nil {
		t.Fatalf("fail to create client connection: %v", err)
	}
	defer closeHelper(conn)

	attrs := map[string]interface{}{"response.code": int64(400)}
	bag := testenv.GetAttrBag(attrs, args.ConfigIdentityAttribute, args.ConfigIdentityAttributeDomain)
	request := mixerapi.ReportRequest{Attributes: []mixerapi.Attributes{ bag}}
	_, err = client.Report(context.Background(), &request)
	if err != nil {
		t.Errorf("fail to send report to Mixer %v", err)
	}
}

func closeHelper(c io.Closer) {
	err := c.Close()
	if err != nil {
		log.Fatal(err)
	}
}

```

Add go_test to the BUILD file adapter/mysampleadapter/BUILD.

Copy the following content into the existing BUILD file. The text in bold shows the newly added content.

<pre>
package(default_visibility = ["//visibility:public"])

load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["mysampleadapter.go"],
    visibility = ["//visibility:public"],
    deps = [
        "//adapter/mysampleadapter/config:go_default_library",
        "//pkg/adapter:go_default_library",
        "//template/metric:go_default_library",
        "@com_github_gogo_protobuf//types:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@com_github_googleapis_googleapis//:google/rpc",
    ],
)

<b>
load("@io_bazel_rules_go//go:def.bzl", "go_test")
go_test(
    name = "go_default_test",
    size = "small",
    srcs = ["mysampleadapter_test.go"],
    library = ":go_default_library",
    data = [
        "sampleoperatorconfig",
    ],
    deps = [
        "//pkg/adapter:go_default_library",
        "//pkg/template:go_default_library",
        "//test/testenv:go_default_library",
        "//template:go_default_library",
        "@io_istio_api//:mixer/v1",  # keep
        "@org_golang_x_net//context:go_default_library",
    ],
)
</b>
</pre>


Build the mixer code and run bazel_to_go.py script to use go tools for testing.

```bash
cd $MIXER_REPO && bazel build ... && ./bin/bazel_to_go.py && go test adapter/mysampleadapter/*.go
```


Inspect the out.txt file that your adapter would have printed inside its own directory.

```bash
tail $MIXER_REPO/adapter/mysampleadapter/out.txt
```

# Step 9: Cleanup

Delete the adapter/mysampleadapter` `directory and undo the edits made inside the adapter/BUILD file.

# Step 10: Next

Next step is to build your own adapter and integrate with  Mixer. Refer to [Developer's guide](./adapters.md) for necessary details.

