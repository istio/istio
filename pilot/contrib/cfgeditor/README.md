# cfgeditor

cfgeditor is a simple platform-independent GUI-oriented editor for Istio configuration files.

You run cfgeditor by giving it the path to the configuration file to edit. It will then
proceed to publish a local web site and launch a web browser showing this site. You can
then edit your config file using this web site.

As it exists right now, cfgeditor is no more no less than just a plain text editor. It
doesn't understand any of the semantics of Istio config files. This functionality will be
added over time as the Istio config format matures. For now, consider cfgeditor as a
proof of concept.

## Notes about the code

The content directory contains the actual web site that the user interacts with
to edit the config. Each HTML and JavaScript file in this directory will be
run through the Go templating logic such that fields from the editorContext
struct can be expanded into the templates.

During the build, the whole web site is ingested and embedded directly into the
cfgeditor binary. This means the binary is stand-alone at runtime.
