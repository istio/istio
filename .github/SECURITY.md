# Security Policy

## Supported Versions

Information about supported Istio versions can be found on the
[Support Announcements] page on Istio's website.

## Reporting a Vulnerability

Instructions for reporting a vulnerability can be found on the
[Istio Security Vulnerabilities] page. The Istio Product Security Working Group receives
vulnerability and security issue reports, and the company affiliation of the members of
the group can be found at [Early Disclosure Membership].

## Security Bulletins

Information about previous Istio vulnerabilities can be found on the
[Security Bulletins] page.

[Support Announcements]: https://istio.io/news/support/
[Istio Security Vulnerabilities]: https://istio.io/about/security-vulnerabilities/
[Security Bulletins]: https://istio.io/news/security/
[Early Disclosure Membership]: https://github.com/istio/community/blob/master/EARLY-DISCLOSURE.md#membership

## Vulnerability Scanning

Istio uses [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck), the official
Go vulnerability scanner, to detect known vulnerabilities in our dependencies. This tool analyzes
the codebase and reports only vulnerabilities in code paths that are actually used, reducing noise
and focusing on real security concerns.

### For Contributors

You can run vulnerability checks locally before submitting a pull request:

```bash
make vuln-check
```

This will scan the entire codebase for known vulnerabilities using the
[Go vulnerability database](https://vuln.go.dev).

### Automated Scanning

Vulnerability scans run automatically:
- On every pull request that modifies Go code or dependencies
- Daily on the main branch
- Results are available in the GitHub Actions workflow artifacts

### Addressing Vulnerabilities

When vulnerabilities are detected:
1. The security team evaluates the impact on Istio
2. High-severity vulnerabilities are addressed promptly
3. Updates are communicated through [Security Bulletins]
4. Fixes are backported to supported releases when appropriate
