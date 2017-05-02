{% if overview %}

{{ overview }}

{% else %}

{% include templates/_errorthrower.md missing_block='overview' purpose='states, in one or two sentences, the purpose of this document' %}

{% endif %}


{% if objectives %}

## Objectives

{{ objectives }}

{% else %}

{% include templates/_errorthrower.md missing_block='objectives' heading='Objectives' purpose='lists the objectives for this tutorial.' %}

{% endif %}


{% if prerequisites %}

## Before you begin

{{ prerequisites }}

{% else %}

{% include templates/_errorthrower.md missing_block='prerequisites' heading='Before you begin' purpose='lists action prerequisites and knowledge prerequisites' %}

{% endif %}


{% if lessoncontent %}

{{ lessoncontent }}

{% else %}

{% include templates/_errorthrower.md missing_block='lessoncontent' purpose='provides the lesson content for this tutorial.' %}

{% endif %}


{% if cleanup %}

## Cleaning up

{{ cleanup }}

{% endif %}


{% if whatsnext %}

## What's next

{{ whatsnext }}

{% endif %}
