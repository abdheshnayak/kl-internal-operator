package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ContainerResource struct {
	Min string `json:"min"`
	Max string `json:"max"`
}

type ContainerEnv struct {
	Key     string `json:"key"`
	Value   string `json:"value,omitempty"`
	Type    string `json:"type,omitempty"`
	RefName string `json:"refName,omitempty"`
	RefKey  string `json:"refKey,omitempty"`
}

type ContainerVolumeItem struct {
	Key      string `json:"key"`
	FileName string `json:"fileName"`
}

type ContainerVolume struct {
	Name      string                `json:"name"`
	MountPath string                `json:"mountPath"`
	Type      string                `json:"type"`
	RefName   string                `json:"refName"`
	Items     []ContainerVolumeItem `json:"items"`
}

type ImageFromGit struct {
}

type AppContainer struct {
	Name            string            `json:"name"`
	Image           string            `json:"image"`
	ImagePullPolicy string            `json:"imagePullPolicy"`
	Command         []string          `json:"command,omitempty"`
	Args            []string          `json:"args,omitempty"`
	ResourceCpu     ContainerResource `json:"resourceCpu"`
	ResourceMemory  ContainerResource `json:"resourceMemory"`
	Env             []ContainerEnv    `json:"env,omitempty"`
	Volumes         []ContainerVolume `json:"volumes,omitempty"`
}

type AppSvc struct {
	Port       uint16 `json:"port"`
	TargetPort uint16 `json:"targetPort,omitempty"`
	Type       string `json:"type,omitempty"`
}

// AppSpec defines the desired state of App
type AppSpec struct {
	Services   []AppSvc       `json:"services,omitempty"`
	Containers []AppContainer `json:"containers"`
}

type ReconPod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Failed    string `json:"failed"`
}

// AppStatus defines the observed state of App
type AppStatus struct {
	Job                  *ReconJob          `json:"job,omitempty"`
	JobCompleted         *bool              `json:"jobCompleted,omitempty"`
	Generation           *int64             `json:"generation,omitempty"`
	DependencyChecked    *map[string]string `json:"dependencyChecked,omitempty"`
	ImagesCheckJob       *ReconPod          `json:"imagesCheckJob,omitempty"`
	ImagesCheckCompleted *bool              `json:"imagesCheckCompleted,omitempty"`
	DeletionJob          *ReconJob          `json:"deletionJob,omitempty"`
	DeletionJobCompleted *bool              `json:"deletionJobCompleted,omitempty"`
	Conditions           []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// App is the Schema for the apps API
type App struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppSpec   `json:"spec,omitempty"`
	Status AppStatus `json:"status,omitempty"`
}

func (app *App) DefaultStatus() {
	app.Status.DependencyChecked = nil
	app.Status.Job = nil
	app.Status.JobCompleted = nil
	app.Status.ImagesCheckJob = nil
	app.Status.ImagesCheckCompleted = nil
	app.Status.Generation = &app.Generation
}

func (app *App) HasJob() bool {
	return app.Status.Job != nil && app.Status.JobCompleted == nil
}

func (app *App) HasNotCheckedDependency() bool {
	return app.Status.DependencyChecked == nil
}

func (app *App) HasPassedDependencyCheck() bool {
	return app.Status.DependencyChecked != nil && len(*app.Status.DependencyChecked) == 0
}

func (app *App) HasNotCheckedImages() bool {
	return app.Status.ImagesCheckCompleted == nil && app.Status.ImagesCheckJob == nil
}

func (app *App) IsCheckingImages() bool {
	// return app.Status.ImagesCheckJob != nil && app.Status.ImagesCheckCompleted == nil
	return app.Status.ImagesCheckJob != nil
}

func (app *App) IsNewGeneration() bool {
	return app.Status.Generation == nil || app.Generation > *app.Status.Generation
}

func (app *App) ShouldCreateJob() bool {
	if app.HasPassedDependencyCheck() && app.Status.JobCompleted == nil && app.Status.Job == nil {
		return true
	}
	return false
}

func (app *App) HasToBeDeleted() bool {
	return app.GetDeletionTimestamp() != nil
}

func (app *App) HasDeletionJob() bool {
	return app.Status.DeletionJob != nil && app.Status.DeletionJobCompleted == nil
}

func (app *App) ShouldCreateDeletionJob() bool {
	if app.Status.DeletionJob == nil && app.Status.DeletionJobCompleted == nil {
		return true
	}
	return false
}

//+kubebuilder:object:root=true

// AppList contains a list of App
type AppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []App `json:"items"`
}

func init() {
	SchemeBuilder.Register(&App{}, &AppList{})
}
