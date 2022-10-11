package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rApi "operators.kloudlite.io/lib/operator.v2"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RegionSpec defines the desired state of Region
type RegionSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of Region. Edit region_types.go to remove/update
	Name    string `json:"name"`
	Account string `json:"account,omitempty"`
}

// RegionStatus defines the observed state of Region
type RegionStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.isReady",description="region"

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
		"kloudlite.io/region":      r.Spec.Name,
		"kloudlite.io/account-ref": r.Spec.Account,
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
