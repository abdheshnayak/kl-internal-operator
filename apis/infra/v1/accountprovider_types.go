package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rApi "operators.kloudlite.io/lib/operator"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type AccountProviderCrediential struct {
	SecretName string `json:"secretName,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Key        string `json:"key,omitempty"`
}

// AccountProviderSpec defines the desired state of AccountProvider
type AccountProviderSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of AccountProvider. Edit accountprovider_types.go to remove/update
	AccountId      string                     `json:"accountId,omitempty"`
	Provider       string                     `json:"provider,omitempty"`
	Region         string                     `json:"region,omitempty"`
	Min            int                        `json:"min,omitempty"`
	Max            int                        `json:"max,omitempty"`
	CredentialsRef AccountProviderCrediential `json:"credentialsRef,omitempty"`
	Pool           string                     `json:"pool,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// AccountProvider is the Schema for the accountproviders API
type AccountProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccountProviderSpec `json:"spec,omitempty"`
	Status rApi.Status         `json:"status,omitempty"`
}

func (a *AccountProvider) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/provider": a.Spec.Provider,
	}
}

func (a *AccountProvider) GetStatus() *rApi.Status {
	return &a.Status
}

//+kubebuilder:object:root=true

// AccountProviderList contains a list of AccountProvider
type AccountProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AccountProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AccountProvider{}, &AccountProviderList{})
}
