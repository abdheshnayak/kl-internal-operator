package v1

import (
	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RegionSpec defines the desired state of Region
type RegionSpec struct {
	AccountName string `json:"accountName,omitempty"`
	IsMaster    bool   `json:"isMaster,omitempty"`
}

// RegionStatus defines the observed state of Region
type RegionStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Account",type="string",JSONPath=".spec.accountName",description="region"
//+kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.isReady",description="region"
//+kubebuilder:printcolumn:name="is_master",type="boolean",JSONPath=".spec.isMaster",description="region"

// Region is the Schema for the regions API
type Region struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RegionSpec  `json:"spec,omitempty"`
	Status rApi.Status `json:"status,omitempty"`
}

func (in *Region) GetEnsuredAnnotations() map[string]string {
	return map[string]string{}
}

func (r *Region) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/region":       r.Name,
		"kloudlite.io/account.name": r.Spec.AccountName,
	}
}

func (r *Region) GetStatus() *rApi.Status {
	return &r.Status
}

//+kubebuilder:object:root=true

// RegionList contains a list of Region
type RegionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Region `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Region{}, &RegionList{})
}
