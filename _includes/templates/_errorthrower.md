### ERROR: You must define a <span style="font-family: monospace">`{{ include.missing_block }}`</span> block
{: style="color:red" }

This template requires that you provide text that {{ include.purpose }}. The text in this block will
be displayed under the heading **{{ include.heading }}**.

To get rid of this message and take advantage of this template, define the `{{ include.missing_block }}`
variable and populate it with content.

```liquid
{% raw %}{%{% endraw %} capture {{ include.missing_block }} {% raw %}%}{% endraw %}
Text that {{ include.purpose }}.
{% raw %}{%{% endraw %} endcapture {% raw %}%}{% endraw %}
```

<!-- TEMPLATE_ERROR -->