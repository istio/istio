# Contribution guidelines

So you want to hack on Istio? Yay! Please refer to Istio's overall
[contribution guidelines](https://github.com/istio/community/blob/master/CONTRIBUTING.md)
to find out how you can help.

## Security and Vulnerability Scanning

Before submitting a pull request, you can optionally run vulnerability checks locally:

```bash
make vuln-check
```

This helps identify known security vulnerabilities in dependencies. See [SECURITY.md](.github/SECURITY.md)
for more information about Istio's vulnerability scanning practices.
