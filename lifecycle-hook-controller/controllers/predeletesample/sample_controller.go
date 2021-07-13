/*
Copyright 2021 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package predeletesample

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	kruiseappspub "github.com/openkruise/kruise-api/apps/pub"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	hookLabelKey = "hook.example.kruise.io/unready-blocker"
)

// SampleReconciler reconciles a Sample object
type SampleReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch

func (r *SampleReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	pod := &v1.Pod{}
	if err := r.Get(context.TODO(), req.NamespacedName, pod); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "failed to get", "pod", req.NamespacedName)
		return ctrl.Result{}, err
	}

	if !isPreDeleteHooked(pod) {
		return ctrl.Result{}, nil
	}

	{
		// your hook logic here, e.g. call an external URL
	}

	// after hook succeeded:
	body := fmt.Sprintf(`{"metadata":{"labels":{"%s":"false"}}}`, hookLabelKey)
	if err := r.Patch(context.TODO(), pod, client.RawPatch(types.StrategicMergePatchType, []byte(body))); err != nil {
		r.Log.Error(err, "failed to patch", "pod", req.NamespacedName)
		return ctrl.Result{}, err
	}

	r.Log.Info("Successfully handle pre-delete hook", "pod", req.NamespacedName)
	return ctrl.Result{}, nil
}

func (r *SampleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: 3}).
		For(&v1.Pod{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				pod := e.Object.(*v1.Pod)
				return isPreDeleteHooked(pod)
			},
			DeleteFunc:  func(_ event.DeleteEvent) bool { return false },
			GenericFunc: func(_ event.GenericEvent) bool { return false },
			UpdateFunc: func(e event.UpdateEvent) bool {
				pod := e.ObjectNew.(*v1.Pod)
				return isPreDeleteHooked(pod)
			},
		}).
		Complete(r)
}

func isPreDeleteHooked(pod *v1.Pod) bool {
	return pod.Labels[kruiseappspub.LifecycleStateKey] == string(kruiseappspub.LifecycleStatePreparingDelete) && pod.Labels[hookLabelKey] == "true"
}
