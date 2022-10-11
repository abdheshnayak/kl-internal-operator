package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rApi "operators.kloudlite.io/lib/operator.v2"
)

type AccountSpec struct {
	AccountId    string   `json:"accountId,omitempty"`
	OwnedDomains []string `json:"ownedDomains,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.isReady",description="region"

// Account is the Schema for the accounts API
type Account struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccountSpec `json:"spec,omitempty"`
	Status rApi.Status `json:"status,omitempty"`
}

func (in *Account) GetEnsuredAnnotations() map[string]string {
	return map[string]string{}
}

func (a *Account) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"accountId": a.Spec.AccountId,
	}
}

func (a *Account) GetStatus() *rApi.Status {
	return &a.Status
}

//+kubebuilder:object:root=true

// AccountList contains a list of Account
type AccountList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Account `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Account{}, &AccountList{})
}
