package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rApi "operators.kloudlite.io/lib/operator.v2"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NodePoolSpec defines the desired state of NodePool
type NodePoolSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	AccountRef string `json:"accountRef,omitempty"`
	EdgeRef    string `json:"edgeRef,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Region     string `json:"region,omitempty"`
	Config     string `json:"config,omitempty"`
	Min        int    `json:"min,omitempty"`
	Max        int    `json:"max,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// NodePool is the Schema for the nodepools API
type NodePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodePoolSpec `json:"spec,omitempty"`
	Status rApi.Status  `json:"status,omitempty"`
}

func (in *NodePool) GetEnsuredAnnotations() map[string]string {
	return map[string]string{}
}

func (a *NodePool) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/node-pool": a.Name,
	}
}

func (a *NodePool) GetStatus() *rApi.Status {
	return &a.Status
}

// +kubebuilder:object:root=true

// NodePoolList contains a list of NodePool
type NodePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodePool{}, &NodePoolList{})
}
