---
title: Doc Templates
headline: Using Page Templates
sidenav: doc-side-home-nav.html
bodyclass: docs
layout: docs
type: markdown
---

Individual topics are built using page templates which provide some consistent
formatting and style to all pages within Istio documentation. 
These page templates are available for writers who would like to contribute new topics to the Istio docs:

- [Concept template](#concept-template)
- [Task template](#task-tempplate)
- [Tutorial template](#tutorial-template)
- [Sample template](#sample-template)

The page templates are in the [_include/templates](https://github.com/istio/istio.github.io/tree/master/_includes/templates) 
directory of the [istio.github.io](https://github.com/istio/istio.github.io) repository.

## Concept template

A concept page explains some significant aspect of Istio. For example, a concept page might describe the 
mixer's configuration model and explain some of its subtleties.
Typically, concept pages don't include sequences of steps, but instead provide links to
tasks or tutorials that do.

To write a new concept page, create a Markdown file in a subdirectory of the
/docs/concepts directory. In your Markdown file,  provide values for these
variables, and then include templates/concept.md:

- `overview` - required
- `body` - required
- `whatsnext` - optional

In the `body` section, use `##` to start with a level-two heading. For subheadings,
use `###` and `####` as needed.

Here's an example of a page that uses the concept template:

{% raw %}
<pre>---
title: Understanding this Thing
headline: Understanding a Thing
sidenav: doc-side-concepts-nav.html
bodyclass: docs
layout: docs
type: markdown
---

{% capture overview %}
This page explains ...
{% endcapture %}

{% capture body %}
## Understanding ...

Istio provides ...

## Using ...

To use ...
{% endcapture %}

{% capture whatsnext %}
* Learn more about [this](...).
* See this [related task](...).
{% endcapture %}

{% include templates/concept.md %}
</pre>
{% endraw %}

Here's an example of a published topic that uses the concept template: [TBD]({{site.baseurl}}/docs/concepts/TBD.html)

## Task template

A task page shows how to do a single thing, typically by giving a short
sequence of steps. Task pages have minimal explanation, but often provide links
to conceptual topics that provide related background and knowledge.

To write a new task page, create a Markdown file in a subdirectory of the
/docs/tasks directory. In your Markdown file, provide values for these
variables, and then include templates/task.md:

- `overview` - required
- `prerequisites` - required
- `steps` - required
- `discussion` - optional
- `whatsnext` - optional

In the `steps` section, use `##` to start with a level-two heading. For subheadings,
use `###` and `####` as needed. Similarly, if you choose to have a `discussion` section,
start the section with a level-two heading.

Here's an example of a Markdown file that uses the task template:

{% raw %}
<pre>---
title: Configuring This Thing
headline: Configuring This Thing
sidenav: doc-side-tasks-nav.html
bodyclass: docs
layout: docs
type: markdown
---

{% capture overview %}
This page shows how to ...
{% endcapture %}

{% capture prerequisites %}
* Do this.
* Do this too.
{% endcapture %}

{% capture steps %}
## Doing ...

1. Do this.
1. Do this next. Possibly read this [related explanation](...).
{% endcapture %}

{% capture discussion %}
## Understanding ...

Here's an interesting thing to know about the steps you just did.
{% endcapture %}

{% capture whatsnext %}
* Learn more about [this](...).
* See this [related task](...).
{% endcapture %}

{% include templates/task.md %}
</pre>
{% endraw %}

Here's an example of a published topic that uses the task template: [TBD]({{site.baseurl}}/docs/tasks/tbd.html)

## Tutorial template

A tutorial page shows how to accomplish a goal that is larger than a single
task. Typically a tutorial page has several sections, each of which has a
sequence of steps. For example, a tutorial might provide a walkthrough of a
code sample that illustrates a certain Istio feature. Tutorials can
include surface-level explanations, but should link to related concept topics
for deep explanations.

To write a new tutorial page, create a Markdown file in a subdirectory of the
/docs/tutorials directory. In your Markdown file, provide values for these
variables, and then include templates/tutorial.md:

- `overview` - required
- `prerequisites` - required
- `objectives` - required
- `lessoncontent` - required
- `cleanup` - optional
- `whatsnext` - optional

In the `lessoncontent` section, use `##` to start with a level-two heading. For subheadings,
use `###` and `####` as needed.

Here's an example of a Markdown file that uses the tutorial template:

{% raw %}
<pre>---
title: Running a Thing
headline: Running a Thing
sidenav: doc-side-tutorials-nav.html
bodyclass: docs
layout: docs
type: markdown
---

{% capture overview %}
This page shows how to ...
{% endcapture %}

{% capture prerequisites %}
* Do this.
* Do this too.
{% endcapture %}

{% capture objectives %}
* Learn this.
* Build this.
* Run this.
{% endcapture %}

{% capture lessoncontent %}
## Building ...

1. Do this.
1. Do this next. Possibly read this [related explanation](...).

## Running ...

1. Do this.
1. Do this next.

## Understanding the code
Here's something interesting about the code you ran in the preceding steps.
{% endcapture %}

{% capture cleanup %}
* Delete this.
* Stop this.
{% endcapture %}

{% capture whatsnext %}
* Learn more about [this](...).
* See this [related tutorial](...).
{% endcapture %}

{% include templates/tutorial.md %}
</pre>
{% endraw %}

Here's an example of a published topic that uses the tutorial template: [TBD]({{site.baseurl}}/docs/tutorials/TBD.html)

## Sample template

A sample page describes a fully working stand-alone example highlighting a particular set of features. Samples
must have easy to follow setup and usage instructions so users can quickly run the sample
themselves and experiment with changing the sample to explore the system.

To write a new sample page, create a Markdown file in a subdirectory of the
/docs/samples directory. In your Markdown file,  provide values for these
variables, and then include templates/sample.md:

- `overview` - required
- `body` - required
- `whatsnext` - optional

In the `body` section, use `##` to start with a level-two heading. For subheadings,
use `###` and `####` as needed.

Here's an example of a page that uses the concept template:

{% raw %}
<pre>---
title: Trying a Thing
headline: Trying a Thing
sidenav: doc-side-samples-nav.html
bodyclass: docs
layout: docs
type: markdown
---

{% capture overview %}
This page explains ...
{% endcapture %}

{% capture body %}
## Running ...

To run this sample...

{% endcapture %}

{% capture whatsnext %}
* Learn more about [this](...).
* See this [related task](...).
{% endcapture %}

{% include templates/sample.md %}
</pre>
{% endraw %}

Here's an example of a published topic that uses the sample template: [TBD]({{site.baseurl}}/docs/samples/TBD.html)
