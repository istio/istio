package manifest

// Names not found in the istio gvk package
const (
	ClusterRole                 = "ClusterRole"
	ClusterRoleBinding          = "ClusterRoleBinding"
	HorizontalPodAutoscaler     = "HorizontalPodAutoscaler"
	NetworkAttachmentDefinition = "NetworkAttachmentDefinition"
	PodDisruptionBudget         = "PodDisruptionBudget"
	Role                        = "Role"
	RoleBinding                 = "RoleBinding"
)

const (
	// MetadataNamespace is the namespace for mesh metadata (labels, annotations)
	MetadataNamespace = "install.operator.istio.io"
	// OwningResourceName represents the name of the owner to which the resource relates
	OwningResourceName = "install.operator.istio.io/owning-resource"
	// OwningResourceNamespace represents the namespace of the owner to which the resource relates
	OwningResourceNamespace = "install.operator.istio.io/owning-resource-namespace"
	// OwningResourceNotPruned indicates that the resource should not be pruned during reconciliation cycles,
	// note this will not prevent the resource from being deleted if the owning resource is deleted.
	OwningResourceNotPruned = "install.operator.istio.io/owning-resource-not-pruned"
	// OperatorManagedLabel indicates Istio operator is managing this resource.
	OperatorManagedLabel = "operator.istio.io/managed"
	// operatorReconcileStr indicates that the operator will reconcile the resource.
	operatorReconcileStr = "Reconcile"
	// IstioComponentLabel indicates which Istio component a resource belongs to.
	IstioComponentLabel = "operator.istio.io/component"
	// OperatorVersionLabel indicates the Istio version of the installation.
	OperatorVersionLabel = "operator.istio.io/version"
)
