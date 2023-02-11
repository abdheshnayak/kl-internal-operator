package v1

import (
	"github.com/kloudlite/internal_operator_v2/lib/constants"
	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CloudProviderSpec defines the desired state of CloudProvider
type CloudProviderSpec struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.isReady",description="region"

// CloudProvider is the Schema for the cloudproviders API
type CloudProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudProviderSpec `json:"spec,omitempty"`
	Status rApi.Status       `json:"status,omitempty"`
}

func (in *CloudProvider) GetEnsuredAnnotations() map[string]string {
	return map[string]string{
		constants.GroupVersionKind: GroupVersion.WithKind("CloudProvider").String(),
	}
}

func (a *CloudProvider) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/provider.name": a.Name,
	}
}

func (a *CloudProvider) GetStatus() *rApi.Status {
	return &a.Status
}

//+kubebuilder:object:root=true

// CloudProviderList contains a list of CloudProvider
type CloudProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudProvider{}, &CloudProviderList{})
}
