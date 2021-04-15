# Test skip configuration

To skip test targets in our ASM test suite we use the `skip.yaml` config file. Using this configuration file we can use selectors to skip tests or packages based on the scenario the test is running under (i.e. cluster topology, workload identity pool type).

For instance, say I want to skip tests `TestSix`, `TestSeven`, and `TestEight` when running under GKE On-Prem. I'd go to the `tests:` section of the configuration and add the following:

```yaml
tests:
  - selectors:
    - cluster_type: gke-on-prem
    targets:
    - names:
        - TestSix
        - TestSeven
        - TestEight
      reason: because seven ate nine
      buganizerID: 132893498
...
...
...
```

And then, if the test is not meant to be skipped, we can add a `reason` and `buganizerID` so that we can keep track of the skips (and maybe make another dashboard that nobody looks at 😃).

For a full list of the test skip selectors consult the code at `tester/pkg/tests/skip.go`.