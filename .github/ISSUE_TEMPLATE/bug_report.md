---
name: Bug report
about: Report a bug to help us improve Istio
---
(**NOTE**: This is used to report product bugs:
  To report a security vulnerability, please visit <https://istio.io/about/security-vulnerabilities>
  To ask questions about how to use Istio, please visit <https://discuss.istio.io>)

**Bug description**

**Affected product area (please put an X in all that apply)**

[ ] Docs
[ ] Installation
[ ] Networking
[ ] Performance and Scalability
[ ] Extensions and Telemetry
[ ] Security
[ ] Test and Release
[ ] User Experience
[ ] Developer Infrastructure
[ ] Upgrade

**Affected features (please put an X in all that apply)**

[ ] Multi Cluster
[ ] Virtual Machine
[ ] Multi Control Plane

**Expected behavior**

**Steps to reproduce the bug**

**Version** (include the output of `istioctl version --remote` and `kubectl version --short` and `helm version --short` if you used Helm)

**How was Istio installed?**

**Environment where the bug was observed (cloud vendor, OS, etc)**

Additionally, please consider running `istioctl bug-report` and attach the generated cluster-state tarball to this issue.
Refer [cluster state archive](http://istio.io/help/bugs/#generating-a-cluster-state-archive) for more details.
