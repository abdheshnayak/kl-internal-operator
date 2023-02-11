package constants

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const MsvcApiVersion = "msvc.kloudlite.io/v1"

const (
	MainNs string = "kl-core"
)

const (
	HelmMongoDBKind string = "HelmMongoDB"

	HelmMySqlDBKind string = "HelmMySqlDB"
	HelmRedisKind   string = "HelmRedis"
)

const (
	CommonFinalizer     string = "finalizers.kloudlite.io"
	ForegroundFinalizer string = "foregroundDeletion"
	KlFinalizer         string = "klouldite-finalizer"
)

var (
	PodGroup = metav1.TypeMeta{
		APIVersion: "v1",
		Kind:       "Pod",
	}

	DeploymentType = metav1.TypeMeta{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
	}

	StatefulsetType = metav1.TypeMeta{
		APIVersion: "apps/v1",
		Kind:       "StatefulSet",
	}

	HelmMongoDBType = metav1.TypeMeta{
		APIVersion: MsvcApiVersion,
		Kind:       "HelmMongoDB",
	}

	HelmRedisType = metav1.TypeMeta{
		APIVersion: MsvcApiVersion,
		Kind:       "HelmRedis",
	}
)

type LoggerType string

const (
	AccountNameKey string = "kloudlite.io/account.name"
	EdgeNameKey    string = "kloudlite.io/edge.name"

	ProjectNameKey       string = "kloudlite.io/project.name"
	MsvcNameKey          string = "kloudlite.io/msvc.name"
	MresNameKey          string = "kloudlite.io/mres.name"
	AppNameKey           string = "kloudlite.io/app.name"
	RouterNameKey        string = "kloudlite.io/router.name"
	LambdaNameKey        string = "kloudlite.io/lambda.name"
	AccountRouterNameKey string = "kloudlite.io/account-router.name"
	// DeviceWgKey          string = "klouldite.io/wg-device-key"
	DeviceWgKey string = "kloudlite.io/is-wg-key"
	WgService   string = "kloudlite.io/wg-service"
	WgDeploy    string = "kloudlite.io/wg-deployment"
	WgDomain    string = "kloudlite.io/wg-domain"

	ProviderNameKey string = "kloudlite.io/provider.name"

	ClearStatusKey string = "kloudlite.io/clear-status"
	RestartKey     string = "kloudlite.io/do-restart"
	NodePoolKey    string = "kloudlite.io/node-pool"
	RegionKey      string = "kloudlite.io/region"
	NodeIndex      string = "kloudlite.io/node-index"
	NodeIps        string = "kloudlite.io/node-ips"

	GroupVersionKind string = "kloudlite.io/group-version-kind"
)

var (
	KnativeServiceType = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "serving.knative.dev/v1",
	}
)

var (
	ConditionReady = struct {
		Type, InitReason, InProgressReason, ErrorReason, SuccessReason string
	}{
		Type:             "Ready",
		InitReason:       "Initialized",
		InProgressReason: "ReconcilationInProgress",
		ErrorReason:      "SomeChecksFailed",
		SuccessReason:    "AllChecksCompleted",
	}
)
