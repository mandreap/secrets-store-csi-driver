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
// These permissions are required for nodePublishSecretRef - but since we don't update them here - to remove

func (r *SecretProviderCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	klog.InfoS("CACHE reconcile started", "spc", req.NamespacedName.String())
	spCacheList := &secretsstorev1.SecretProviderCacheList{}
	if err := r.reader.List(ctx, spCacheList, r.ListOptionsLabelSelector()); err != nil {
		klog.ErrorS(err, "Failed to list SecretProviderCache")
		return ctrl.Result{}, err
	}
	spCaches := spCacheList.Items
	for i := range spCaches {
		spCache := spCaches[i]
		namespace := spCache.ObjectMeta.Namespace
		if spCache.Spec.SpcFilesWorkloads == nil {
			spCache.Status.WarningNoPersistencyOnRestart = false
			err := r.Status().Update(ctx, &spCache)
			if err != nil {
				klog.ErrorS(err, "Failed to update Status for SecretProviderCache")
				return ctrl.Result{}, err
			}
		}
		var warning bool = false
		cacheSpcWorkloadFiles := spCache.Spec.SpcFilesWorkloads
		spcName := spCache.Spec.SecretProviderClassName
		//klog.InfoS("Secret Provider Class Name", "spcName", spcName)
		spc := &secretsstorev1.SecretProviderClass{}
		err := r.reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: spcName}, spc)
		if err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		if errors.IsNotFound(err) {
			//TODO: if in online mode remove the spc from the cache
			klog.Warning("Can't find SPC: %s", spcName)
		}
		shouldUpdateCache := false
		mapOfPodsToDelete := make(map[string]string)
		for _, cacheWorkload := range cacheSpcWorkloadFiles.WorkloadsMap {
			klog.InfoS("Checking pods", "CachedPods", cacheWorkload.CachedPods)
			//TODO: refactor these into functions
			if cacheWorkload.OwnerReferenceKind == "Pod" {
				klog.InfoS("Workload is a Pod", "workload = ", cacheWorkload.WorkloadName)
				podName := cacheWorkload.WorkloadName
				klog.InfoS("Checking pod", "pod", podName)
				pod := &corev1.Pod{}
				err = r.reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod)
				if err != nil && !errors.IsNotFound(err) {
					klog.ErrorS(err, "failed to get pod", "pod", klog.ObjectRef{Namespace: req.Namespace, Name: podName})
					return ctrl.Result{RequeueAfter: 10 * time.Second}, err
				}
				if apierrors.IsNotFound(err) {
					klog.InfoS("pod not found - removing it from the list", "pod", podName)
					// if map is nil -> delete is a noop
					//delete(cacheSpcWorkloadFiles.WorkloadsMap, podName)
					mapOfPodsToDelete[cacheWorkload.WorkloadName] = cacheWorkload.WorkloadName
					shouldUpdateCache = true
					continue
				}
				// remove the pod if it is being terminated
				// or the pod is in succeeded state (for jobs that complete aren't gc yet)
				// or the pod is in a failed state (all containers get terminated)
				if !pod.GetDeletionTimestamp().IsZero() || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
					klog.InfoS("pod terminating - removing it from the list", "pod", podName)
					mapOfPodsToDelete[cacheWorkload.WorkloadName] = cacheWorkload.WorkloadName
					shouldUpdateCache = true
					continue
				}
				for _, ownerRef := range pod.OwnerReferences {
					if ownerRef.Kind != "Pod" {
						break
					}
					// can we have a deployment/replicaset etc set as owner of a pod after the pod was created?
					klog.InfoS("Pod owner references", "ownerRef", ownerRef.Name, "ownerRefUID", ownerRef.UID)
					podHash, ok := pod.Labels["pod-template-hash"]
					if ownerRef.Kind != "Pod" && ok && strings.Contains(ownerRef.Name, podHash) && cacheWorkload.WorkloadName == podName {
						cacheWorkload.OwnerReferenceName = ownerRef.Name
						cacheWorkload.OwnerReferenceKind = ownerRef.Kind
						cacheWorkload.OwnerReferenceUID = string(ownerRef.UID)
						cacheWorkload.WorkloadName = strings.ReplaceAll(ownerRef.Name, podHash, "")
						cacheWorkload.WorkloadName = strings.TrimRight(cacheWorkload.WorkloadName, "-")
						klog.InfoS("Changing the workload kind and name in the cache:", "workloadName", cacheWorkload.WorkloadName, "workloadKind", cacheWorkload.OwnerReferenceKind)
						shouldUpdateCache = true
						break
					}
				}
			}
			if len(cacheWorkload.CachedPods) == 0 {
				klog.InfoS("No pods found for workload", "workload", cacheWorkload.WorkloadName)
				continue
			}
			sliceOfPodsToDelete := make(map[string]string)
			for podName := range cacheWorkload.CachedPods {
				klog.InfoS("Checking pod", "pod", podName)
				pod := &corev1.Pod{}
				err = r.reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod)
				if err != nil && !errors.IsNotFound(err) {
					klog.ErrorS(err, "failed to get pod", "pod", klog.ObjectRef{Namespace: req.Namespace, Name: podName})
					return ctrl.Result{RequeueAfter: 10 * time.Second}, err
				}

				if apierrors.IsNotFound(err) {
					klog.InfoS("pod not found - removing it from the list", "pod", podName)
					// if map is nil -> delete is a noop
					sliceOfPodsToDelete[podName] = podName
					shouldUpdateCache = true
					continue
				}

				// remove the pod if it is being terminated
				// or the pod is in succeeded state (for jobs that complete aren't gc yet)
				// or the pod is in a failed state (all containers get terminated)
				if !pod.GetDeletionTimestamp().IsZero() || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
					klog.InfoS("pod terminating - removing it from the list", "pod", podName)
					sliceOfPodsToDelete[podName] = podName
					shouldUpdateCache = true
					continue
				}
				/*
					// can we have a deployment/replicaset etc set as owner of a pod after the pod was created?
					// this should be a rare case and we should do the change in the associated map of workloads
					for _, ownerRef := range pod.OwnerReferences {
						if ownerRef.Kind != "Pod" {
							break
						}
						klog.InfoS("Pod owner references", "ownerRef", ownerRef.Name, "ownerRefUID", ownerRef.UID)
						podHash, ok := pod.Labels["pod-template-hash"]
						if ownerRef.Kind != "Pod" && ok && strings.Contains(ownerRef.Name, podHash) && cacheWorkload.WorkloadName == podName {
							cacheWorkload.OwnerReferenceName = ownerRef.Name
							cacheWorkload.OwnerReferenceKind = ownerRef.Kind
							cacheWorkload.OwnerReferenceUID = string(ownerRef.UID)
							cacheWorkload.WorkloadName = strings.ReplaceAll(ownerRef.Name, podHash, "")
							cacheWorkload.WorkloadName = strings.TrimRight(cacheWorkload.WorkloadName, "-")
							klog.InfoS("Changing the workload kind and name in the cache:", "workloadName", cacheWorkload.WorkloadName, "workloadKind", cacheWorkload.OwnerReferenceKind)
							shouldUpdateCache = true
							break
						}
					}*/
			}
			// erase all the pods which aren't longer in the cluster
			// todo: add a check to do this only if we're in the "online" mode
			for podName := range sliceOfPodsToDelete {
				klog.InfoS("Removing pod from the cache", "pod", podName)
				delete(spCache.Spec.SpcFilesWorkloads.WorkloadsMap[cacheWorkload.WorkloadName].CachedPods, podName)
			}

			for _, cacheWorkload := range cacheSpcWorkloadFiles.WorkloadsMap {
				if cacheWorkload.OwnerReferenceKind == "Pod" {
					warning = true
				}
			}
		}

		// erase all the workloads which aren't in the cluster
		for workloadName := range mapOfPodsToDelete {
			klog.InfoS("Removing workload from the cache", "workload", workloadName)
			delete(spCache.Spec.SpcFilesWorkloads.WorkloadsMap, workloadName)
		}

		if shouldUpdateCache {
			klog.InfoS("Updating the cache", "cache name", spCache.Name)
			err = r.writer.Update(ctx, &spCache)
			if err != nil {
				return ctrl.Result{RequeueAfter: 10 * time.Second}, err
			}
		}

		klog.InfoS("Updating status to:", "warning", warning)
		spCache.Status.WarningNoPersistencyOnRestart = warning
		err = r.Status().Update(ctx, &spCache)
		if err != nil {
			klog.ErrorS(err, "Failed to update Status for SecretProviderCache")
			return ctrl.Result{RequeueAfter: 60 * time.Second}, err
		}
	}

	klog.InfoS("CACHE reconcile completed", "spc", req.NamespacedName.String())

	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
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
