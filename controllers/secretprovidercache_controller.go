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
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
		reader:        mgr.GetCache(),
		writer:        mgr.GetClient(),
		eventRecorder: recorder,
	}, nil
}

// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretprovidercaches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretprovidercaches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretproviderclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretproviderclasses/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="storage.k8s.io",resources=csidrivers,verbs=get;list;watch,resourceNames=secrets-store.csi.k8s.io
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// These permissions are required for nodePublishSecretRef - but since we don't update them here - to remove

func (r *SecretProviderCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	klog.InfoS("CACHE reconcile started", "spc", req.NamespacedName.String())
	spCacheList := &secretsstorev1.SecretProviderCacheList{}
	if err := r.reader.List(ctx, spCacheList); err != nil {
		klog.ErrorS(err, "Failed to list SecretProviderCache")
		return ctrl.Result{}, err
	}
	spCaches := spCacheList.Items
	for i := range spCaches {
		spCache := spCaches[i]
		namespace := spCache.ObjectMeta.Namespace
		// check if the associated spc is deleted
		spcName := spCache.Spec.SecretProviderClassName
		spc := &secretsstorev1.SecretProviderClass{}
		err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: spcName}, spc)
		if err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		if errors.IsNotFound(err) && len(spcName) > 0 {
			klog.InfoS("SPC not found - removing the cache", "spc", spcName)
			err = r.Delete(ctx, &spCache)
			if err != nil {
				klog.ErrorS(err, "Failed to delete SecretProviderCache")
				return ctrl.Result{RequeueAfter: 1 * time.Minute}, err
			}
			continue
		}

		err = r.Status().Update(ctx, &spCache)
		if err != nil {
			klog.ErrorS(err, "Failed to update Status for SecretProviderCache")
			return ctrl.Result{RequeueAfter: 60 * time.Second}, err
		}
	}

	klog.InfoS("CACHE reconcile completed", "spc", req.NamespacedName.String())

	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

func (r *SecretProviderCacheReconciler) ownerWasErased() predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldSecretProviderCache, ok := e.ObjectOld.(*secretsstorev1.SecretProviderCache)
			if !ok {
				return false
			}
			newSecretProviderCache, ok := e.ObjectNew.(*secretsstorev1.SecretProviderCache)
			if !ok {
				return false
			}
			return oldSecretProviderCache.ObjectMeta.OwnerReferences != nil && newSecretProviderCache.ObjectMeta.OwnerReferences == nil
		},
	}
}

func (r *SecretProviderCacheReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&secretsstorev1.SecretProviderCache{}).
		WithEventFilter(r.ownerWasErased()).
		Complete(r)
}
