# Notes

## Prep Steps

The issues we will suggest tackling today can be viewed at https://github.com/istio/istio/issues?q=is%3Aissue+state%3Aopen+label%3Acontribfest-26.

1. Launch a Codespace from your fork of istio/istio
1. Checkout a feature branch from master, call it what you'd like. Perhaps `contribfest`

```shell
export TAG=1.32-dev
export HUB=gcr.io/istio-testing
./prow/integ-suite-kind.sh --skip-cleanup --skip-build
```

These commands set up a running KinD cluster with a local container registry, ready to run any `istioctl` command.

## Install Istio

```shell
# Build istioctl binary
make -B $(pwd)/out/linux_amd64/release/istioctl-linux-amd64
# Convenience alias
alias istioctl="$(pwd)/out/linux_amd64/release/istioctl-linux-amd64"
istioctl install --set profile=ambient --skip-confirmation --set tag=$TAG --set hub=$HUB
# waypoints are Kubernetes Gateway API Resources, install the CRDs
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.7.0/standard-install.yaml
```

These commands install Istio with the ambient profile and set up the necessary Gateway API CRDs.

## Setup and test a basic playground

```shell
kubectl create namespace server
kubectl label namespace server istio.io/dataplane-mode=ambient
kubectl apply -f ./samples/httpbin/httpbin.yaml -n server
kubectl label -n server svc httpbin istio.io/use-waypoint=waypoint
kubectl create namespace client
kubectl label namespace client istio.io/dataplane-mode=ambient
kubectl apply -f ./samples/sleep/sleep.yaml -n client
```

Check the environment:

```shell
kubectl exec -n client deploy/sleep -it -- curl -v http://httpbin.server:8000/headers
kubectl logs -n istio-system ds/ztunnel | grep outbound | tail -1
```

Deploy a waypoint for the server namespace:

```shell
istioctl waypoint apply -n server
istioctl waypoint status -n server --waypoint-timeout=10s
```

## Reproduce the issue

<!-- Each issue has its own reproduction steps. Follow the notes for your chosen issue. -->

### Issue [#55987](https://github.com/istio/istio/issues/55987): `istioctl analyze` should warn when no default revision or tag exists

**Background.** A revision is selected by the `istio.io/rev=<rev>` label on a namespace/pod/gateway. Workloads with *no* rev label are injected by the **default** revision or tag, which is represented by a `MutatingWebhookConfiguration` labelled `istio.io/rev=default` (default revision) or `istio.io/tag=default` (default tag, created with `istioctl tag set default --revision <rev>`). If none exists, unlabelled gateways/namespaces are injected by nothing, and on upgrade two revisions can fight over them. `analyze` should warn about this.

**Reproduce.** The ambient install above also creates a default injector webhook (`istio.io/rev=default`), so it would hide the bug. Remove it and install only a named revision (no default), then analyze:

```shell
istioctl uninstall --purge -y
istioctl install --set revision=canary --set tag=$TAG --set hub=$HUB --skip-confirmation
kubectl get mutatingwebhookconfigurations -l istio.io/rev --show-labels   # no istio.io/rev=default / istio.io/tag=default
istioctl analyze -A      # BUG: no warning about missing default
istioctl tag set default --revision canary --yes   # fixes the situation; the warning should disappear
```

**Where to look.**

- `pkg/config/analysis/analyzers/webhook/webhook.go`: the existing webhook analyzer; `isIstioWebhook` and `isDefaultRevisionedWebhook` are useful helpers.
- `pkg/config/analysis/msg/messages.yaml`: message definitions (last code is `IST0178`). Add a new one (`IST0179`), then regenerate `messages.gen.go` with `go generate ./pkg/config/analysis/msg/` (needs `goimports` on your PATH; or run `gofmt -w` on the generated file).
- `pkg/config/analysis/analyzers/all.go`: register any new analyzer here (`TestAnalyzersInAll` enforces it).
- `pkg/config/analysis/analyzers/analyzers_test.go` plus `testdata/*.yaml`: table-driven analyzer tests.

**Design hints / gotchas.**

- Existing `webhook.Analyzer` is also used by the operator (`operator/pkg/webhook/webhook.go`, with `SkipDefaultRevisionedWebhook`), so a separate analyzer avoids changing that behavior.
- Only consider webhooks where `isIstioWebhook` is true, and only warn if at least one exists (don't warn on clusters with no Istio injection webhooks at all). Note that a default install, including ambient, creates an `istio.io/rev=default` webhook, so it must not trigger the warning.
- A default *tag* webhook (from `istioctl tag set default`) carries `istio.io/tag=default` and `istio.io/rev=<target revision>`, so check both labels.
- Known limitation (see closed [PR #57179](https://github.com/istio/istio/pull/57179) discussion): `istioctl analyze --revision X` only feeds that revision's webhooks to analyzers, so the default-tag webhook can be missed. Discuss options with maintainers: default `--revision` to empty, add `--all-revisions`, or soften the message. Our patch ignores this and checks every webhook the analyzer sees.
- Name the analyzer after what it checks (e.g. `DefaultRevisionAnalyzer`), following other analyzers such as `telemetry.DefaultSelectorAnalyzer`.
- Test three cases with fixtures in `testdata/`: no default (expect `IST0179`), default revision, and default tag (expect no messages). Run them with `go test ./pkg/config/analysis/analyzers/ -run 'TestAnalyzers/webhook'`.
- Stretch goal: a stale default tag whose `istio.io/rev` target has no webhook still suppresses the warning. Can you detect it?
- Add a release note (`releasenotes/notes/55987.yaml`, `kind: feature`, `area: istioctl`).

**How to test your fix.**

1. *Unit tests (fast, no cluster).* Add fixtures under `pkg/config/analysis/analyzers/testdata/` and one entry per scenario in `analyzers_test.go`. Each entry lists input files, the analyzer, and the expected `{message type, resource}` pairs (an empty list means no warnings):

   | Fixture | Webhook labels | Expected |
   | --- | --- | --- |
   | `webhook_no_default.yaml` | `istio.io/rev: canary` | `IST0179` on the webhook |
   | `webhook_default.yaml` | `istio.io/rev: default` | no messages |
   | `webhook_default_tag.yaml` | `istio.io/tag: default`, `istio.io/rev: canary` | no messages |

   ```shell
   go test ./pkg/config/analysis/analyzers/ -run 'TestAnalyzers/webhook' -v
   # also run the registration and metadata checks
   go test ./pkg/config/analysis/analyzers/ -run 'TestAnalyzersInAll|TestAnalyzersHaveUniqueNames|TestAnalyzersHaveDescription'
   ```

   Sanity check: temporarily break your analyzer (for example, never report) and confirm the no-default test fails.

1. *Try it without a cluster.* `istioctl analyze --use-kube=false` reads only the local files, so you can test a fixture directly (after you create it, or apply `tests.patch`; rebuild istioctl first):

   ```shell
   istioctl analyze --use-kube=false pkg/config/analysis/analyzers/testdata/webhook_no_default.yaml   # expect IST0179
   istioctl analyze --use-kube=false pkg/config/analysis/analyzers/testdata/webhook_default.yaml      # expect no IST0179
   ```

1. *On the cluster.* Rebuild istioctl (see the development loop) and follow the reproduction steps:

   | State | Command | Expected |
   | --- | --- | --- |
   | Only revision `canary` installed | `istioctl analyze -A` | `IST0179` warning |
   | After `istioctl tag set default --revision canary --yes` | `istioctl analyze -A` | no `IST0179` |
   | Normal install (`istioctl install --set profile=ambient ...`) | `istioctl analyze -A` | no `IST0179` |

1. *Before you push.* Run the whole analysis tree and the linter:

   ```shell
   go test ./pkg/config/analysis/... ./istioctl/pkg/analyze/...
   make lint-go
   ```

## A reasonable development loop

```shell
# Make your code change in the relevant file (see the issue for guidance)
# Build the binary you modified — istioctl or istiod, depending on the issue:
#   istioctl:
make -B $(pwd)/out/linux_amd64/release/istioctl-linux-amd64
#   istiod:
make -B $(pwd)/out/linux_amd64/release/pilot-discovery
kubectl -n istio-system set image deployment/istiod discovery=$(pwd)/out/linux_amd64/release/pilot-discovery

# Run the unit tests for the package you changed
go test -p 1 -race ./path/to/affected/package/...

# Verify the fix on the cluster by reproducing the steps from the section above
```

## Automated Testing

To ensure your fix is not broken by future changes, Istio will ask that tests be included. Unit tests live alongside the code they cover — look for a `_test.go` file in the same package as the file you modified. Add test cases that exercise the scenario from the issue and verify the correct behavior.

Run the tests for your package with:

```shell
go test -p 1 -race ./path/to/affected/package/...
```

## If you are stuck

If you're feeling stuck, you can ask one of the maintainers or contributors involved in the session for help. We've also included patches to help get you unstuck. `fix.patch` contains a proposed fix, you can look at it for a hint, or apply it directly with `git apply fix.patch` if you'd rather move on. Additionally, `tests.patch` contains suggested test cases that you can apply with `git apply tests.patch`. For issue #55987, `fix.patch` adds a `webhook.DefaultRevisionAnalyzer` (message `IST0179`, registration, release note) and `tests.patch` adds three cases to `analyzers_test.go` (no default, default revision, default tag) plus the `testdata/webhook_*.yaml` fixtures. Run just those with `go test ./pkg/config/analysis/analyzers/ -run 'TestAnalyzers/webhook'`.

## Release notes

Well formatted and clear release notes are a critical part of helping the Istio release managers produce timely and well documented releases. This helps users of Istio know what's new, improved or different about the release which includes your work.

In Istio, release notes are found in `releasenotes/notes`. Commonly, the note's file name will be the number of the issue which produced the work you've done. The contents are yaml explaining the change. In our case, it might look something like this:

```yaml
apiVersion: release-notes/v2
kind: bug-fix
area: <area>  # e.g. istioctl, ambient, security
issue:
- <issue-number>
releaseNotes:
- |
    **Fixed** <short description of what was broken and what the fix does>.
```

This file should be included in a commit in your branch when you file a PR. CI will check for a release note and the test fails if one has not been provided. If you forget, that's ok. You can simply create a release note, commit it and push again using the steps shown in the next section. Every push to your branch after the PR is opened will trigger new tests in CI, including running the release note check.

## Add, commit, push

As you make progress, it's good to save your work. Let's walk through how to use git to publish your contribution.

```shell
# first, see what files you have changed
git status
# use git to view a diff if you are unsure what was changed
git diff
# next, add the files you wish to publish
git add <your changed files>
# after this, make a commit and give it a message to help you identify the contents later
git commit -m "<short description of your change>"
# finally, publish your work to your fork by pushing
git push -u origin <name of your branch>
```

Once your code is pushed to your fork, you can move on to filing a Pull Request, commonly referred to as a PR. We'll walk through the PR process in slides.

## BONUS CONTENT: On building your own Istio images

In a Codespace, building your own Istio images can be slow — on the order of 10 minutes or more depending on your machine type. We've chosen to skip this step and instead use a 1.32 dev build created by Istio's CI whenever a PR merges to the `master` branch. This doesn't mean that building an Istio image is difficult though, and it's a good skill to understand if you're looking to work on other portions of the codebase, such as pilot (istiod). In this section we'll talk briefly about how to build and run your own images.

First, let's adjust the environment variables which control what we're building and installing:

```shell
export TAG=istio-testing
export HUB=localhost:5000
export DOCKER_TARGETS='docker.pilot docker.proxyv2 docker.ztunnel docker.install-cni'
export DOCKER_BUILD_VARIANTS="distroless"
```

Next, setup a new kind cluster. This time we won't skip build:

```shell
./prow/integ-suite-kind.sh --skip-cleanup
```

The tag here could be whatever you want, but the HUB is rather interesting. When you used these arguments for the prow script to build your KinD environment, you may have noticed there's an extra registry container running in docker. This registry image lets you build and push images to your local development environment. It requires a little bit of special plumbing in KinD which is also taken care of for you by the prow scripts.

Now, let's build our images. The script already built some images, but it didn't follow all of our directives, so we'll build our own now. We set the desired targets above to be simply the core images needed for a basic ambient mesh installation. There are other images which would normally be built that enable our integration tests, but for now we can reduce the number of images so this progresses faster.

```shell
make docker.push
```

When you run this you'll get information about what it's going to try to do, and it'll handle building and pushing images for you.

```text
2026-11-07T13:44:17.623450Z     info    Args: Push:              true
Save:              false
NoClobber:         false
NoCache:           false
Targets:           [pilot proxyv2 ztunnel install-cni]
Variants:          [distroless]
Architectures:     [linux/amd64]
BaseVersion:       master-2026-10-21T15-15-01
BaseImageRegistry: gcr.io/istio-release
IstioVersion:      1.32-dev
Tags:              [istio-testing]
Hubs:              [localhost:5000]
Builder:           docker
```

Notice that it reads tags, hubs, targets and variants correctly from environment variables.

The next step would be installing using these images. If Istio is already installed in your local KinD, you could edit deployments and daemon sets to use these images.

```shell
istioctl install --set profile=ambient --skip-confirmation --set tag=$TAG --set hub=$HUB --set values.global.imagePullPolicy=Always
```

**PRO TIP:** By default, we'll install using the `IfNotPresent` imagePullPolicy. When building your local development loop, this can cause you some frustrations. After the first time a pod starts, the `istio-testing` tag will be present on the node and Kubernetes will stop pulling updated images when you push them! It's strongly advised to change this to `Always`. Notice, in the install command shown above we've taken care of this setting.

From here, you are ready to start a development loop by making changes, running `make docker.push`, and restarting the containers you're working on. Give it a try. `newDiscoveryCommand` in `pilot/cmd/pilot-discovery/app/cmd.go` is the entry point for our istiod image. Try adding a log message to its RunE and then check out the logs after your newly built istiod starts up. You can tighten this loop further by only building the images you're working on, but I'll leave this as an exercise for the reader to figure out.
