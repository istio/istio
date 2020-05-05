# Feature Coverage Reporting

## Overview

The features package defines values that are used to track feature coverage at eng.istio.io

## Labeling a Test

Adding a label to a test using the integration framework is easy.  Simply call the Feature() function on the Test Object, passing any relevant feature constants as parameters.  If you are testing a feature that does not have a constant defined, see the next section for how to define one.

## Adding New Feature Constants

For consistency, feature constants are generated from `features.yaml`.  Each entry in this file will be generated into a dot delimited feature label.  For instance:

```yaml
    foo:
      bar
      baz:
        boo
```

will generate constants like

```go
    const (
        foo_bar = "foo.bar"
        boo_baz_boo = "foo.baz.boo"
    )
```

The heirarchical nature of feature labels allows us to aggregate data about related feature sets.  To provide for consistent reporting and aggregation, we have defined the top two layers of heirarchy, and ask that you place your feature under these headings.  For more detail on the purpose of each heading, see Top-Level Feature Headings below.  If you feel that none of the existing headings fit your feature, please check with the Test and Release Working Group before adding a new heading.

## Writing a Test Stub

Test stubs are a valuable way to indicate that a feature should be tested, but isn't yet.  When writing a design doc, a set of test stubs can represent your Test Plan.  You can also use stubs to document features whose lack of testing needs to be visible in our feature coverage dashboard.

To write a stub, simply create a test object and call NotImplementedYet, passing the label of any features this test should cover.  This test gap will now appear in our feature coverage dashboards, which give release managers a better understanding of what is and isn't tested in a given release.  If you are implementing a test stub with no immediate plans to implement the test, it's a best practice to create a tracking issue as well.

## Top-Level Feature Headings

For reporting purposes, we organize our features under a handful of high level categories, which should change very rarely, so that reports stay consistent.  This section will document the top level categories to help you decide where to place your feature label.

- Observability - features that relate to measuring or exploring your service mesh or the services which comprise it.
  - Telemetry
  - Tracing

- Security - features that allow users to secure their services and service mesh.
  - Peer
    - AuthN
    - AuthZ
  - User
    - AuthN
    - AuthZ

- Control - features relating to controling their service mesh and services.
  - Routing
  - Short-Circuiting
  - Mirroring

- Operations - features related to operating Istio itself, rather than the service mesh or individual services.
  - Observability
  - Security
