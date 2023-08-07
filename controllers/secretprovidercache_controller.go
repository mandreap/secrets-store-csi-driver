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
	"k8s.io/apimachinery/pkg/api/errors"
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
	spCacheList := &secretsstorev1.SecretProviderCacheList{}
	if err := r.reader.List(ctx, spCacheList, r.ListOptionsLabelSelector()); err != nil {
		klog.ErrorS(err, "failed to list SecretProviderCache")
		return ctrl.Result{}, err
	}
	spCaches := spCacheList.Items
	for i := range spCaches {
		spCache := spCaches[i]
		namespace := spCache.ObjectMeta.Namespace
		//serviceAccount := spCache.Spec.ServiceAccountName
		for _, cachePodSpcMap := range spCache.Spec.WorkloadSecretsMap {
			for podName, _ := range cachePodSpcMap.PodsName {
				klog.InfoS("podName", "podName", podName)
				pod := &corev1.Pod{}
				err := r.reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod)
				if err != nil && !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				if errors.IsNotFound(err) {
					klog.InfoS("pod not found", "pod", podName)
					if len(cachePodSpcMap.PodsName) > 1 {
						delete(cachePodSpcMap.PodsName, podName)
					}
					//TODO: we need to check we're in the online mode here to remove all pods from the cache
					r.writer.Update(ctx, &spCache)
				}
				for _, ownerRef := range pod.OwnerReferences {
					klog.InfoS("Pod owner references", "ownerRef", ownerRef.Name, "ownerRefUID", ownerRef.UID)
				}
				klog.InfoS("pod", "pod labels", pod.Labels)
				klog.InfoS("pod", "pod annotations", pod.Annotations)

				// TODO: check if we're in online mode here and if we are then check if pod is running
				// check if pod is running
				if pod.Status.Phase == corev1.PodRunning {
					klog.InfoS("pod is running", "pod", pod.Name)

				}
				// if the pod is not running, then we need to check if the pod is in the cache, remove its associated data,
				// and then update the cache
			}
		}
	}

	klog.InfoS("CACHE reconcile completed", "spc", req.NamespacedName.String())

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
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
