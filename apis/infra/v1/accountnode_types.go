package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rApi "operators.kloudlite.io/lib/operator"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AccountNodeSpec defines the desired state of AccountNode
type AccountNodeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of AccountNode. Edit accountnode_types.go to remove/update
	AccountRef string `json:"accountRef,omitempty"`
	// Region      string `json:"region,omitempty"`
	ProviderRef string `json:"providerRef,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Config      string `json:"config,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// AccountNode is the Schema for the accountnodes API
type AccountNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccountNodeSpec `json:"spec,omitempty"`
	Status rApi.Status     `json:"status,omitempty"`
}

func (a *AccountNode) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/account-node.name": a.Name,
		"kloudlite.io/account-ref":       a.Spec.AccountRef,
		"kloudlite.io/provider-ref":      a.Spec.AccountRef,
		// "kloudlite.io/region":       a.Spec.Region,
	}
}

func (a *AccountNode) GetStatus() *rApi.Status {
	return &a.Status
}

//+kubebuilder:object:root=true

// AccountNodeList contains a list of AccountNode
type AccountNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccountNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AccountNode{}, &AccountNodeList{})
}
