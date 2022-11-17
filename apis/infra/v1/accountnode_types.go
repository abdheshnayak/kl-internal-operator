package v1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"operators.kloudlite.io/lib/constants"
	rApi "operators.kloudlite.io/lib/operator"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AccountNodeSpec defines the desired state of AccountNode
type AccountNodeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of AccountNode. Edit accountnode_types.go to remove/update
	AccountRef  string `json:"accountRef,omitempty"`
	Region      string `json:"region"`
	EdgeRef     string `json:"edgeRef"`
	Provider    string `json:"provider"`
	ProviderRef string `json:"providerRef,omitempty"`
	Config      string `json:"config"`
	Pool        string `json:"pool"`
	// +kubebuilder:default=0
	Index int `json:"nodeIndex,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Index",type="integer",JSONPath=".spec.nodeIndex",description="index of node"
// +kubebuilder:printcolumn:name="Account",type="string",JSONPath=".spec.accountRef",description="account"
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider",description="provider"
// +kubebuilder:printcolumn:name="Region",type="string",JSONPath=".spec.region",description="region"
// +kubebuilder:printcolumn:JSONPath=".metadata.creationTimestamp",name=Age,type=date

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
		constants.AccountRef:             a.Spec.AccountRef,
		"kloudlite.io/region":            a.Spec.EdgeRef,
		constants.NodePoolKey:            a.Spec.Pool,
		constants.NodeIndex:              fmt.Sprintf("%d", a.Spec.Index),
		"kloudlite.io/provider-ref":      a.Spec.ProviderRef,
	}
}

func (a *AccountNode) GetStatus() *rApi.Status {
	return &a.Status
}

// +kubebuilder:object:root=true

// AccountNodeList contains a list of AccountNode
type AccountNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccountNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AccountNode{}, &AccountNodeList{})
}
