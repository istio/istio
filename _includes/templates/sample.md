{% if overview %}

{{ overview }}

{% else %}

{% include templates/_errorthrower.md missing_block='overview' purpose='states, in one or two sentences, the purpose of this document' %}

{% endif %}


{% if prerequisites %}

## Before you begin

{{ prerequisites }}

{% else %}

{% include templates/_errorthrower.md missing_block='prerequisites' heading='Before you begin' purpose='lists action prerequisites and knowledge prerequisites' %}

{% endif %}


{% if discussion %}

{{ discussion }}

{% else %}

{% include templates/_errorthrower.md missing_block='discussion' purpose='supplies the discussion of the page content.' %}

{% endif %}


{% if whatsnext %}

## What's next

{{ whatsnext }}

{% endif %}
