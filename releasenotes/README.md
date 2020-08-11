# Istio Release Notes

This directory contains the release notes, upgrade notes, and security notes for Istio.
Notes should be created as part of the pull request for any user facing changes. Before
a release, the release notes utility will be run in order to generate a release notes file
which will be reviewed by the release managers and documentation team.

## Adding a Release Note

To create a release note, create a new file in the [./notes](./notes) directory based on
the [template](./template.yaml). The filename doesn't matter as long as it ends with a `.yaml`
extension and matches the format specified in the template. However, please make names descriptive.

```yaml
apiVersion: release-notes/v2
kind: bug-fix
area: traffic-management

# issue is a list of GitHub issues resolved in this note.
issue:
  - https://github.com/istio/istio/issues/23622
  - 23624
releaseNotes:
- |
*Fixed* an issue preventing the operator from recreating watched resources if they are deleted

upgradeNotes:
  - title: Change the readiness port of gateways
    content: |
      If you are using the 15020 port to check the health of your Istio ingress gateway with your Kubernetes network load balancer, change the port from 15020 to 15021.

securityNotes:
- |
__[CVE-2020-15104](https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-2020-15104)__:
When validating TLS certificates, Envoy incorrectly allows a wildcard DNS Subject Alternative Name to apply to multiple subdomains. For example, with a SAN of `*.example.com`, Envoy incorrectly allows `nested.subdomain.example.com`, when it should only allow `subdomain.example.com`.
    - CVSS Score: 6.6 [AV:N/AC:H/PR:H/UI:N/S:C/C:H/I:L/A:N/E:F/RL:O/RC:C](https://nvd.nist.gov/vuln-metrics/cvss/v3-calculator?vector=AV:N/AC:H/PR:H/UI:N/S:C/C:H/I:L/A:N/E:F/RL:O/RC:C&version=3.1)
```

Some release notes may affect multiple types of notes. For those, please fill in all respective areas. For notes that only affect one or two areas, please fill in only those sections. Sections that don't have content can be omitted.

### Area

This field describes the are of Istio that the note affects. Valid values include:
* traffic-management
* security
* telemetry
* installation
* istioctl
* documentation

### Issue

While many pull requests will only fix a single GitHub issue, some pull requests may fix multiple issues. Please list all fixed GitHub issues. Issues written as numbers only will be interpreted as being reported against the `istio/istio` repo, while issues recorded as URLs will be read as the supplied URLs.

### Release Notes

These notes detail bug fixes, feature additions, removals, or other general content that has an impact to users. Release notes should be written in complete sentences, and the first word should be an action presented in the format `*Action*`.

### Upgrade Notes

These notes detail the changes which purposefully break backwards compatibility with the previous version of Istio. The notes also mention changes which preserve backwards compatibility while introducing new behavior. Changes are only included if the new behavior would be unexpected to a user. These should be written in paragraph format with a title and content.

### Security Notes

These notes detail fixes to security issues in Istio. These may be upgrades to vulnerable libraries, fixes for CVEs, or related content. Security Notes should start with the first the CVE ID in the format `__[CVE-2020-15104](https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-2020-15104)__` followed by a description and then the CVSS score in the format `CVSS Score: 6.6 [AV:N/AC:H/PR:H/UI:N/S:C/C:H/I:L/A:N/E:F/RL:O/RC:C](https://nvd.nist.gov/vuln-metrics/cvss/v3-calculator?vector=AV:N/AC:H/PR:H/UI:N/S:C/C:H/I:L/A:N/E:F/RL:O/RC:C&version=3.1)`

## Adding a release note to multiple Istio Releases

Just as code fixes should be added to master first, notes should as well. To add a note to multiple releases, just cherrypick them over to the appropriate release and the release notes tooling will include them in its generation.
