package constants

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const MsvcApiVersion = "msvc.kloudlite.io/v1"

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

const (
	AccountRef string = "kloudlite.io/account-ref"
	ProjectRef string = "kloudlite.io/project-ref"

	ProjectNameKey       string = "kloudlite.io/project.name"
	MsvcNameKey          string = "kloudlite.io/msvc.name"
	MresNameKey          string = "kloudlite.io/mres.name"
	AppNameKey           string = "kloudlite.io/app.name"
	RouterNameKey        string = "kloudlite.io/router.name"
	LambdaNameKey        string = "kloudlite.io/lambda.name"
	AccountRouterNameKey string = "kloudlite.io/account-router.name"

	ClearStatusKey string = "kloudlite.io/clear-status"
	RestartKey     string = "kloudlite.io/do-restart"
	NodePoolKey    string = "kloudlite.io/node-pool"
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
