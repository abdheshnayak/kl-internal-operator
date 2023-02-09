package v1

import (
	"fmt"

	"github.com/kloudlite/internal_operator_v2/lib/constants"
	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// WorkerNodeSpec defines the desired state of WorkerNode
type WorkerNodeSpec struct {
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

type WorkerNodeStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// WorkerNode is the Schema for the workernodes API
type WorkerNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkerNodeSpec `json:"spec,omitempty"`
	Status rApi.Status    `json:"status,omitempty"`
}

func (a *WorkerNode) GetEnsuredAnnotations() map[string]string {
	return map[string]string{}
}

func (a *WorkerNode) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/account-node.name": a.Name,
		constants.AccountRef:             a.Spec.AccountRef,
		"kloudlite.io/region":            a.Spec.EdgeRef,
		constants.NodePoolKey:            a.Spec.Pool,
		constants.NodeIndex:              fmt.Sprintf("%d", a.Spec.Index),
		"kloudlite.io/provider-ref":      a.Spec.ProviderRef,
	}
}

func (a *WorkerNode) GetStatus() *rApi.Status {
	return &a.Status
}

//+kubebuilder:object:root=true

// WorkerNodeList contains a list of WorkerNode
type WorkerNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkerNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkerNode{}, &WorkerNodeList{})
}
