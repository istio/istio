# Feature Coverage Reporting

## Overview

The features package defines values that are used to track feature coverage at eng.istio.io

## Labeling a Test

Labeling a test with a feature and scenario allow release managers and others to understand what value this test case is adding.  To label a test, call the Feature() function on the Test Object, with a string identifying the feature and scenario you are testing.  These parameters should be formatted like featurepath.scenariopath, where both feature path and scenario path are dot delimited heirarchies, and the featurepath is a leaf node in features.yaml.  If you are testing a feature whose path does not yet exist in features,yaml, see the next section for how to define one.

## Adding New Feature Constants

For consistency, features must be registered in [features.yaml](features.yaml), or your test will fail.  Each entry in this file will be equivalent to a dot delimited feature label.  For instance:

```yaml
    usability:
      observability:
        status:
      analyzers:
        virtualservice:
```

will allow you to label a test with:

```go
    "usability.observability.status.exist-by-default"
    "usability.analyzers.virtualservice"
```

where "usability.observability.status" is the feature, and "exist-by-default" is the scenario in the first case, and the second case has no scenario.

The heirarchical nature of feature labels allows us to aggregate data about related feature sets.  To provide for consistent reporting and aggregation, we have defined the top two layers of hierarchy, and ask that you place your feature under these headings.  For more detail on the purpose of each heading, see Top-Level Feature Headings below.  If you feel that none of the existing headings fit your feature, please check with the Test and Release Working Group before adding a new heading.

## Writing a Test Stub

Test stubs are a valuable way to indicate that a feature should be tested, but isn't yet.  When writing a design doc, a set of test stubs can represent your Test Plan.  You can also use stubs to document features whose lack of testing needs to be visible in our feature coverage dashboard.

To write a stub, simply create a test object and call NotImplementedYet, passing the label of any features this test should cover.  This test gap will now appear in our feature coverage dashboards, which give release managers a better understanding of what is and isn't tested in a given release.  If you are implementing a test stub with no immediate plans to implement the test, it's a best practice to create a tracking issue as well.  If your test stub uses a new feature constant, be sure to follow the instructions above to update our list of features.

```go
  func TestExample(t *testing.T) {
    framework.NewTest(t).NotImplementedYet("my.feature.string")
  }
```