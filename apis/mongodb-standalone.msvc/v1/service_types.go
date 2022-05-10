package v1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"operators.kloudlite.io/lib"
)

// ServiceSpec defines the desired state of Service
type ServiceSpec struct {
}

// ServiceStatus defines the observed state of Service
type ServiceStatus struct {
	Conditions lib.Conditions `json:"inline,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Service is the Schema for the services API
type Service struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceSpec   `json:"spec,omitempty"`
	Status ServiceStatus `json:"status,omitempty"`
}

func (s *Service) NameRef() string {
	return fmt.Sprintf("%s/%s/%s", s.GroupVersionKind().Group, s.Namespace, s.Name)
}

// +kubebuilder:object:root=true

// ServiceList contains a list of Service
type ServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Service `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Service{}, &ServiceList{})
}
