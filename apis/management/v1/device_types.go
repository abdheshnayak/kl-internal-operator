package v1

import (
	"fmt"

	"github.com/kloudlite/internal_operator_v2/lib/constants"
	rApi "github.com/kloudlite/internal_operator_v2/lib/operator.v2"
	"github.com/seancfoley/ipaddress-go/ipaddr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Port struct {
	Port       int32 `json:"port,omitempty"`
	TargetPort int32 `json:"targetPort,omitempty"`
}

// DeviceSpec defines the desired state of Device
type DeviceSpec struct {
	AccountName  string `json:"accountName"`
	ActiveRegion string `json:"activeRegion,omitempty"`
	Offset       int    `json:"offset"`
	// DeviceId     string `json:"deviceId"`
	// DeviceName string `json:"deviceName"`
	Ports []Port `json:"ports,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ip",type="string",JSONPath=".metadata.annotations.ip",description="provider"
// +kubebuilder:printcolumn:name="Active Region",type="string",JSONPath=".spec.activeRegion",description="provider"
// +kubebuilder:printcolumn:name="Account",type="string",JSONPath=".spec.accountId",description="provider"
//+kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.isReady",description="region"

// Device is the Schema for the devices API
type Device struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeviceSpec  `json:"spec,omitempty"`
	Status rApi.Status `json:"status,omitempty"`
}

func (d *Device) GetEnsuredAnnotations() map[string]string {
	return map[string]string{
		constants.GroupVersionKind: GroupVersion.WithKind("Device").String(),

		"ip": func() string {
			is, err := getRemoteDeviceIp(int64(d.Spec.Offset))
			if err != nil {
				fmt.Println(err)
				return ""
			}
			return is.String()
		}(),
	}
}

func (d *Device) GetEnsuredLabels() map[string]string {
	return map[string]string{
		"kloudlite.io/device.name":  d.Name,
		"kloudlite.io/account.name": d.Spec.AccountName,
	}
}

func (d *Device) GetStatus() *rApi.Status {
	return &d.Status
}

func getRemoteDeviceIp(deviceOffset int64) (*ipaddr.IPAddressString, error) {
	deviceRange := ipaddr.NewIPAddressString("10.13.0.0/16")

	if address, addressError := deviceRange.ToAddress(); addressError == nil {
		increment := address.Increment(deviceOffset + 2)
		return ipaddr.NewIPAddressString(increment.GetNetIP().String()), nil
	} else {
		return nil, addressError
	}
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
