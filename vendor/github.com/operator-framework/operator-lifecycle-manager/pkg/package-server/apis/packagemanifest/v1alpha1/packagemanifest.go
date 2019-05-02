package v1alpha1

import operatorsv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"

// CreateCSVDescription creates a CSVDescription from a given CSV
func CreateCSVDescription(csv *operatorsv1alpha1.ClusterServiceVersion) CSVDescription {
	desc := CSVDescription{
		DisplayName: csv.Spec.DisplayName,
		Version:     csv.Spec.Version,
		Provider: AppLink{
			Name: csv.Spec.Provider.Name,
			URL:  csv.Spec.Provider.URL,
		},
		Annotations:     csv.GetAnnotations(),
		LongDescription: csv.Spec.Description,
	}

	icons := make([]Icon, len(csv.Spec.Icon))
	for i, icon := range csv.Spec.Icon {
		icons[i] = Icon{
			Base64Data: icon.Data,
			Mediatype:  icon.MediaType,
		}
	}

	if len(icons) > 0 {
		desc.Icon = icons
	}

	return desc
}
