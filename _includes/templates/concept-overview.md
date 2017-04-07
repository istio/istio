{% if concept %}<!-- check for this before going any further; if not present, skip to else at bottom -->

# Overview of {{concept}}s

{% if what_is %}

### What is a {{ concept }}?

{{ what_is }}

{% else %}

{% include templates/_errorthrower.md missing_block='what_is' heading='What is a (Concept)?' purpose='explains what this concept is and its purpose.' %}

{% endif %}


{% if when_to_use %}

### When to use {{ concept }}s

{{ when_to_use }}

{% else %}

{% include templates/_errorthrower.md missing_block='when_to_use' heading='When to use (Concept)' purpose='explains when to use this object.' %}

{% endif %}


{% if when_not_to_use %}

### When not to use {{ concept }}s

{{ when_not_to_use }}

{% else %}

{% include templates/_errorthrower.md missing_block='when_not_to_use' heading='When not to use (Concept)' purpose='explains when not to use this object.' %}

{% endif %}


{% if status %}

### {{ concept }} status

{{ status }}

{% else %}

{% include templates/_errorthrower.md missing_block='status' heading='Retrieving status for a (Concept)' purpose='explains how to retrieve a status description for this object.' %}

{% endif %}


{% if usage %}

#### Usage

{{ usage }}

{% else %}

{% include templates/_errorthrower.md missing_block='usage' heading='Usage' purpose='shows the most basic, common use case for this object, in the form of a code sample, command, etc, using tabs to show multiple approaches' %}

{% endif %}

<!-- continuing the "if concept" if/then: -->

{% else %}

### ERROR: You must define a "concept" variable
{: style="color:red" }

This template requires a variable called `concept` that is simply the name of the
concept for which you are giving an overview. This will be displayed in the 
headings for the document.

To get rid of this message and take advantage of this template, define `concept`:

```liquid
{% raw %}{% assign concept="Replication Controller" %}{% endraw %}
```

Complete this task, then we'll walk you through preparing the rest of the document.

{% endif %}
