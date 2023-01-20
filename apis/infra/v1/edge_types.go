package v1

import (
	"github.com/kloudlite/internal_operator_v2/lib/constants"
	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type AccountProviderCrediential struct {
	SecretName string `json:"secretName,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Key        string `json:"key,omitempty"`
}

type Pool struct {
	Name   string `json:"name,omitempty"`
	Config string `json:"config,omitempty"`
	Min    int    `json:"min,omitempty"`
	Max    int    `json:"max,omitempty"`
}

// EdgeSpec defines the desired state of Edge
type EdgeSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of Edge. Edit accountprovider_types.go to remove/update
	AccountId      string                     `json:"accountId,omitempty"`
	Provider       string                     `json:"provider,omitempty"`
	Region         string                     `json:"region,omitempty"`
	CredentialsRef AccountProviderCrediential `json:"credentialsRef,omitempty"`
	Pools          []Pool                     `json:"pools,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// Edge is the Schema for the accountproviders API
type Edge struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EdgeSpec    `json:"spec,omitempty"`
	Status rApi.Status `json:"status,omitempty"`
}

func (in *Edge) GetEnsuredAnnotations() map[string]string {
	return map[string]string{
		constants.GroupVersionKind: GroupVersion.WithKind("Edge").String(),
	}
}

func (a *Edge) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/provider":     a.Spec.Provider,
		"kloudlite.io/account-ref":  a.Spec.AccountId,
		"kloudlite.io/edge-ref":     a.Name,
		"kloudlite.io/provider-ref": a.Spec.CredentialsRef.SecretName,
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
