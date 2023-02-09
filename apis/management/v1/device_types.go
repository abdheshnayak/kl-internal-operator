package v1

import (
	"github.com/kloudlite/internal_operator_v2/lib/constants"
	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Port struct {
	Port       int32 `json:"port,omitempty"`
	TargetPort int32 `json:"targetPort,omitempty"`
}

// DeviceSpec defines the desired state of Device
type DeviceSpec struct {
	AccountId    string `json:"accountId"`
	ActiveRegion string `json:"activeRegion,omitempty"`
	Offset       int    `json:"offset"`
	// DeviceId     string `json:"deviceId"`
	// DeviceName string `json:"deviceName"`
	Ports []Port `json:"ports,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// Device is the Schema for the devices API
type Device struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeviceSpec  `json:"spec,omitempty"`
	Status rApi.Status `json:"status,omitempty"`
}

func (in *Device) GetEnsuredAnnotations() map[string]string {
	return map[string]string{
		constants.GroupVersionKind: GroupVersion.WithKind("Device").String(),
	}
}

func (d *Device) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/device-ref":  d.Name,
		"kloudlite.io/account-ref": d.Spec.AccountId,
	}
}

func (d *Device) GetStatus() *rApi.Status {
	return &d.Status
}

// +kubebuilder:object:root=true

// DeviceList contains a list of Device
type DeviceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Device `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Device{}, &DeviceList{})
}
