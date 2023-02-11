package v1

import (
	"fmt"

	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodePoolSpec defines the desired state of NodePool
type NodePoolSpec struct {
	AccountName  string `json:"accountName"`
	ClusterName  string `json:"clusterName"`
	EdgeName     string `json:"edgeName,omitempty"`
	Provider     string `json:"provider,omitempty"`
	ProviderName string `json:"providerName,omitempty"`
	Region       string `json:"region,omitempty"`
	Config       string `json:"config,omitempty"`
	Min          int    `json:"min,omitempty"`
	Max          int    `json:"max,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Account",type="string",JSONPath=".spec.accountRef",description="account"
// +kubebuilder:printcolumn:name="Provider/Region",type="string",JSONPath=".metadata.annotations.provider-region",description="provider"
// +kubebuilder:printcolumn:name="Min/Max",type="string",JSONPath=".metadata.annotations.min-max",description="index of node"
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.isReady",description="region"

// NodePool is the Schema for the nodepools API
type NodePool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodePoolSpec `json:"spec,omitempty"`
	Status rApi.Status  `json:"status,omitempty"`
}

func (a *NodePool) GetEnsuredAnnotations() map[string]string {
	return map[string]string{
		"min-max":         fmt.Sprintf("%d/%d", a.Spec.Min, a.Spec.Max),
		"provider-region": fmt.Sprintf("%s/%s", a.Spec.Provider, a.Spec.Region),
	}
}

func (a *NodePool) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/node-pool":     a.Name,
		"kloudlite.io/provider.name": a.Spec.ProviderName,
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
