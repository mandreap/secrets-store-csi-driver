/*
Copyright 2022 The Kubernetes Authors.

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

package controllers

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"sigs.k8s.io/controller-runtime/pkg/predicate"
	secretsstorev1 "sigs.k8s.io/secrets-store-csi-driver/apis/v1"
)

// SecretProviderCacheReconciler reconciles a SecretProviderCache object
type SecretProviderCacheReconciler struct {
	client.Client
	Log    logr.Logger
	nodeID string
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io.secrets-store.csi.k8s.io,resources=secretprovidercaches,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io.secrets-store.csi.k8s.io,resources=secretprovidercaches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretproviderclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="storage.k8s.io",resources=csidrivers,verbs=get;list;watch,resourceNames=secrets-store.csi.k8s.io
 
// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SecretProviderCache object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.6.4/pkg/reconcile
func (r *SecretProviderCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("secretprovidercache", req.NamespacedName)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretProviderCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&secretsstorev1.SecretProviderCache{}).
		Complete(r)
}

// belongsToNodePredicate defines predicates for handlers
func (r *SecretProviderCacheReconciler) belongsToNodePredicate() predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return r.processIfBelongsToNode(e.ObjectNew)
		},
		CreateFunc: func(e event.CreateEvent) bool {
			return r.processIfBelongsToNode(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return r.processIfBelongsToNode(e.Object)
		},
	}
}

// processIfBelongsToNode determines if the secretproviderclasspodstatus belongs to the node based on the
// internal.secrets-store.csi.k8s.io/node-name: <node name> label. If belongs to node, then the spcps is processed.
func (r *SecretProviderCacheReconciler) processIfBelongsToNode(objMeta metav1.Object) bool {
	node, ok := objMeta.GetLabels()[secretsstorev1.InternalNodeLabel]
	if !ok {
		return false
	}
	if !strings.EqualFold(node, r.nodeID) {
		return false
	}
	return true
}
