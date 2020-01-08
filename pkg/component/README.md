The component package directory defines an in-memory representation of the IstioOperatorSpec proto and follows
the layout of the proto closely.

The purpose of the representation is to programmatically reference the IstioOperatorSpec and perform functions 
related to it, like rendering a manifest.

The top level is an IstioOperator, containing IstioFeatures, which in turn contain IstioComponents. 
 
The structure of features and components is embedded in the code and reflects the IstioOperatorSpec proto so, 
for example, TrafficManagement feature contains Pilot and Proxy components, just as the proto does.
A related, but not exactly equal mapping is between component names and helm charts. This mapping is defined in 
a map and represents the layout of the charts directory structure. 

Given the structures and directory mappings in the code, the steps executed in rendering a manifest for an IstioOperator are 
as follows:

1. Create a new IstioOperator with an *IstioOperatorSpec, which internally creates a slice of features for the
control plane, each of which recursively creates slices of components belonging to that feature. Each component
internally creates a helm renderer. The IstioOperatorSpec is assumed to be a final, overlaid tree, formed by
patching a user overlay IstioOperatorSpec over a base IstioOperatorSpec (associated with a profile). This 
overlaying is done prior to passing in the IstioOperatorSpec.
1. Run the control plane, which starts activities like monitoring helm charts for changes. 
1. Calling RenderManifest calls each of the features' RenderManifest, which in turn call each of the feature's
component's RenderManifest and concatenates the results.
1. The helm chart render step is done at the IstioComponent level (since a chart roughly corresponds to a component).
The rendering is done in a number of steps:
   1. charts and base global values have already been loaded into the helm renderer when it was started
   1. ValueOverlays are patched from IstioOperatorSpec and the resulting YAML tree passed in to helm render
   function. This further overlays the passed in values over the previously loaded global values base. 
   1. The resulting YAML text is patched with any k8sObjectOverlay entries in IstioOperatorSpec.
 
