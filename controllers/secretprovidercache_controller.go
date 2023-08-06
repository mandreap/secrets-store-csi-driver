/*
Copyright 2020 The Kubernetes Authors.

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
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	secretsstorev1 "sigs.k8s.io/secrets-store-csi-driver/apis/v1"
	"sigs.k8s.io/secrets-store-csi-driver/pkg/client/clientset/versioned/scheme"
)

// SecretProviderCacheReconciler reconciles a SecretProviderCache object
type SecretProviderCacheReconciler struct {
	client.Client
	mutex         *sync.Mutex
	scheme        *apiruntime.Scheme
	nodeID        string
	reader        client.Reader
	writer        client.Writer
	eventRecorder record.EventRecorder
}

// New creates a new SecretProviderCacheReconciler
func NewSecretProviderCacheReconciler(mgr manager.Manager, nodeID string) (*SecretProviderCacheReconciler, error) {
	eventBroadcaster := record.NewBroadcaster()
	kubeClient := kubernetes.NewForConfigOrDie(mgr.GetConfig())
	eventBroadcaster.StartRecordingToSink(&clientcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "csi-secrets-store-controller"})

	return &SecretProviderCacheReconciler{
		Client:        mgr.GetClient(),
		mutex:         &sync.Mutex{},
		scheme:        mgr.GetScheme(),
		nodeID:        nodeID,
		reader:        mgr.GetCache(),
		writer:        mgr.GetClient(),
		eventRecorder: recorder,
	}, nil
}

// ListOptionsLabelSelector returns a ListOptions with a label selector for node name.
func (r *SecretProviderCacheReconciler) ListOptionsLabelSelector() client.ListOption {
	return client.MatchingLabels(map[string]string{
		secretsstorev1.InternalNodeLabel: r.nodeID,
	})
}

/*
func (r *SecretProviderCacheReconciler) RunPatcher(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Patcher(ctx); err != nil {
				klog.ErrorS(err, "failed to patch SecretProviderCache")
			}
		}
	}
}

func (r *SecretProviderCacheReconciler) Patcher(ctx context.Context) error {
	klog.V(10).Info("SecretProviderCacheReconciler patcher started")
	r.mutex.Lock()
	defer r.mutex.Unlock()

	klog.V(10).Info("SecretProviderCacheReconciler patcher completed")
	return nil
}*/

// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretprovidercaches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretprovidercaches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretproviderclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretproviderclasses/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretproviderclasspodstatuses,verbs=get;list;watch
// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretproviderclasspodstatuses/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="storage.k8s.io",resources=csidrivers,verbs=get;list;watch,resourceNames=secrets-store.csi.k8s.io
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// These permissions are required for nodePublishSecretRef

func (r *SecretProviderCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	klog.InfoS("CACHE reconcile started", "spc", req.NamespacedName.String())
	spCache := &secretsstorev1.SecretProviderCache{}
	if err := r.reader.Get(ctx, req.NamespacedName, spCache); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("reconcile complete", "spCache", req.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "failed to get secret provider cache", "spCache", req.NamespacedName.String())
		return ctrl.Result{}, err
	}

	klog.InfoS("CACHE reconcile completed", "spc", req.NamespacedName.String())

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *SecretProviderCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&secretsstorev1.SecretProviderCache{}).
		WithEventFilter(r.belongsToNodePredicate()).
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

// processIfBelongsToNode determines if the SecretProviderCache belongs to the node based on the
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

// generateEvent generates an event
func (r *SecretProviderCacheReconciler) generateEvent(obj apiruntime.Object, eventType, reason, message string) {
	if obj != nil {
		r.eventRecorder.Eventf(obj, eventType, reason, message)
	}
}
