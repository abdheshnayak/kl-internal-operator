package v1

import (
	"fmt"

	"github.com/kloudlite/internal_operator_v2/lib/constants"
	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type Pool struct {
	Name   string `json:"name"`
	Config string `json:"config"`
	Min    int    `json:"min,omitempty"`
	Max    int    `json:"max,omitempty"`
}

// EdgeSpec defines the desired state of Edge
type EdgeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of Edge. Edit accountprovider_types.go to remove/update
	AccountName  string `json:"accountName"`
	Provider     string `json:"provider,omitempty"`
	Region       string `json:"region,omitempty"`
	ProviderName string `json:"providerName"`
	ClusterName  string `json:"clusterName"`

	Pools []Pool `json:"pools,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Account",type="string",JSONPath=".spec.accountName",description="account"
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider",description="provider"
// +kubebuilder:printcolumn:name="Clsuter",type="string",JSONPath=".spec.clusterName",description="provider"
// +kubebuilder:printcolumn:name="pools",type="string",JSONPath=".metadata.annotations.node-pools-count",description="index of node"
//+kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.isReady",description="region"

// Edge is the Schema for the accountproviders API
type Edge struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EdgeSpec    `json:"spec,omitempty"`
	Status rApi.Status `json:"status,omitempty"`
}

func (a *Edge) GetEnsuredAnnotations() map[string]string {
	return map[string]string{
		constants.GroupVersionKind: GroupVersion.WithKind("Edge").String(),
		"node-pools-count":         fmt.Sprint(len(a.Spec.Pools)),
	}
}

func (a *Edge) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/provider":      a.Spec.Provider,
		"kloudlite.io/account.name":  a.Spec.AccountName,
		"kloudlite.io/edge.name":     a.Name,
		"kloudlite.io/provider.name": a.Spec.ProviderName,
	}
}

func (a *Edge) GetStatus() *rApi.Status {
	return &a.Status
}

//+kubebuilder:object:root=true

// EdgeList contains a list of Edge
type EdgeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Edge `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Edge{}, &EdgeList{})
}
