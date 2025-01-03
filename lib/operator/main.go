package operator

import (
	"context"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kloudlite/internal_operator_v2/lib/conditions"
	"github.com/kloudlite/internal_operator_v2/lib/constants"
	fn "github.com/kloudlite/internal_operator_v2/lib/functions"
	"github.com/kloudlite/internal_operator_v2/lib/logger"
	rawJson "github.com/kloudlite/internal_operator_v2/lib/raw-json"
)

// +kubebuilder:object:generate=true

type Status struct {
	IsReady          bool               `json:"isReady"`
	DisplayVars      rawJson.RawJson    `json:"displayVars,omitempty"`
	GeneratedVars    rawJson.RawJson    `json:"generatedVars,omitempty"`
	Conditions       []metav1.Condition `json:"conditions,omitempty"`
	StatusConditions []metav1.Condition `json:"statusConditions,omitempty"`
	OpsConditions    []metav1.Condition `json:"opsConditions,omitempty"`
	Message          string             `json:"message,omitempty"`
}

type Resource interface {
	client.Object
	runtime.Object
	GetStatus() *Status
	GetEnsuredLabels() map[string]string
	GetEnsuredAnnotations() map[string]string
}

type Request[T Resource] struct {
	ctx    context.Context
	client client.Client
	Object T
	Logger *zap.SugaredLogger
	locals map[string]any
}

type stepResult struct {
	result *ctrl.Result
	err    error
}

func GetLocal[T any, V Resource](r *Request[V], key string) (T, bool) {
	x := r.locals[key]
	t, ok := x.(T)
	if !ok {
		return *new(T), ok
	}
	return t, ok
}

func HasLocal[V Resource](r *Request[V], key string) bool {
	_, ok := r.locals[key]
	return ok

}

func SetLocal[T any, V Resource](r *Request[V], key string, value T) {
	r.locals[key] = value
}

func NewStepResult(result *ctrl.Result, err error) StepResult {
	return &stepResult{result: result, err: err}
}

func (s stepResult) Err() error {
	return s.err
}

func (s stepResult) Result() ctrl.Result {
	if s.result == nil {
		return ctrl.Result{}
	}
	return *s.result
}

func (s stepResult) ShouldProceed() bool {
	return s.result == nil && s.err == nil
}

type StepResult interface {
	Err() error
	Result() ctrl.Result
	ShouldProceed() bool
}

func NewRequest[T Resource](ctx context.Context, c client.Client, nn types.NamespacedName,
	resInstance T) *Request[T] {
	if err := c.Get(ctx, nn, resInstance); err != nil {
		return nil
	}
	return &Request[T]{
		ctx:    ctx,
		client: c,
		Object: resInstance,
		Logger: logger.New(nn),
		locals: map[string]any{},
	}
}

func (r *Request[T]) EnsureFinilizer(finalizerName string) StepResult {
	controllerutil.AddFinalizer(r.Object, finalizerName)
	return NewStepResult(nil, r.client.Update(r.ctx, r.Object))
}

func (r *Request[T]) EnsureLabels() StepResult {
	el := r.Object.GetEnsuredLabels()

	if !fn.MapContains(r.Object.GetLabels(), el) {
		x := r.Object.GetLabels()
		if x == nil {
			x = map[string]string{}
		}

		for k, v := range el {
			x[k] = v
		}

		return NewStepResult(&ctrl.Result{}, r.client.Update(r.ctx, r.Object))

	}

	return NewStepResult(nil, nil)
}

func (r *Request[T]) EnsureAnnotations() StepResult {
	el := r.Object.GetEnsuredAnnotations()

	if !fn.MapContains(r.Object.GetAnnotations(), el) {
		x := r.Object.GetAnnotations()
		if x == nil {
			x = map[string]string{}
		}

		for k, v := range el {
			x[k] = v
		}

		return NewStepResult(&ctrl.Result{}, r.client.Update(r.ctx, r.Object))

	}

	return NewStepResult(nil, nil)
}

func (r *Request[T]) FailWithStatusError(err error) StepResult {
	e := ""
	if err != nil {
		e = err.Error()
	}
	newConditions, _, err2 := conditions.Patch(
		r.Object.GetStatus().StatusConditions, []metav1.Condition{
			{
				Type:    "FailedWithErr",
				Status:  metav1.ConditionFalse,
				Reason:  "StatusFailedWithErr",
				Message: e,
			},
		},
	)
	if err2 != nil {
		return NewStepResult(nil, err2)
	}

	r.Object.GetStatus().StatusConditions = newConditions
	r.Object.GetStatus().IsReady = false
	err3 := r.client.Status().Update(r.ctx, r.Object)
	if err3 != nil {
		return NewStepResult(&ctrl.Result{}, err3)
	}
	return NewStepResult(&ctrl.Result{}, err)
}

func (r *Request[T]) FailWithOpError(err error) StepResult {
	newConditions, _, err := conditions.Patch(
		r.Object.GetStatus().OpsConditions, []metav1.Condition{
			{
				Type:    "FailedWithErr",
				Status:  metav1.ConditionFalse,
				Reason:  "OpsFailedWithErr",
				Message: err.Error(),
			},
		},
	)
	if err != nil {
		return NewStepResult(nil, err)
	}

	r.Object.GetStatus().OpsConditions = newConditions
	r.Object.GetStatus().IsReady = false
	err2 := r.client.Status().Update(r.ctx, r.Object)
	if err2 != nil {
		return NewStepResult(
			&ctrl.Result{
				Requeue: true,
			}, err2,
		)
	}
	return NewStepResult(&ctrl.Result{}, err)
}

func (r *Request[T]) Context() context.Context {
	return r.ctx
}

func (r *Request[T]) Done(result ...*ctrl.Result) StepResult {
	r.Object.GetStatus().OpsConditions = []metav1.Condition{}
	r.Object.GetStatus().StatusConditions = []metav1.Condition{}

	err3 := r.client.Status().Update(r.ctx, r.Object)
	if err3 != nil {
		return NewStepResult(&ctrl.Result{}, err3)
	}

	if len(result) > 0 {
		return NewStepResult(result[0], nil)
	}
	return NewStepResult(&ctrl.Result{}, nil)
}

func (r *Request[T]) Next() StepResult {
	return NewStepResult(nil, nil)
}

func (r *Request[T]) Finalize() StepResult {
	controllerutil.RemoveFinalizer(r.Object, constants.KlFinalizer)
	controllerutil.RemoveFinalizer(r.Object, constants.CommonFinalizer)
	controllerutil.RemoveFinalizer(r.Object, constants.ForegroundFinalizer)
	return NewStepResult(&ctrl.Result{}, r.client.Update(r.ctx, r.Object))
}

func Get[T client.Object](ctx context.Context, cli client.Client, nn types.NamespacedName, obj T) (T, error) {
	if err := cli.Get(ctx, nn, obj); err != nil {
		return obj, err
	}
	return obj, nil
}
