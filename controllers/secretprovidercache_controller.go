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
	"fmt"
	"strings"
	"sync"
	"time"

	secretsstorev1 "sigs.k8s.io/secrets-store-csi-driver/apis/v1"
	"sigs.k8s.io/secrets-store-csi-driver/pkg/client/clientset/versioned/scheme"

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
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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


// New creates a new SecretProviderClassPodStatusReconciler
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

func (r *SecretProviderCacheReconciler) RunPatcher(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Patcher(ctx); err != nil {
				klog.ErrorS(err, "failed to patch cache")
			}
		}
	}
}

func (r *SecretProviderCacheReconciler) Patcher(ctx context.Context) error {
	klog.V(10).Info("CACHE - patcher started")
	r.mutex.Lock()
	defer r.mutex.Unlock()

	spCacheList := &secretsstorev1.SecretProviderCacheList{}
	spcMap := make(map[string]secretsstorev1.SecretProviderClass)
	//secretOwnerMap := make(map[types.NamespacedName][]metav1.OwnerReference)
	// get a list of all spCaches that belong to the node
	err := r.reader.List(ctx, spCacheList, r.ListOptionsLabelSelector())
	if err != nil {
		return fmt.Errorf("failed to list cache, err: %w", err)
	}

	spCacheStatuses := spCacheList.Items
	for i := range spCacheStatuses {
		secretProviderClassName := spCacheStatuses[i].Status.SecretProviderClassName
		secretProviderClass := &secretsstorev1.SecretProviderClass{}
		namespace := spCacheStatuses[i].Namespace

		if val, exists := spcMap[namespace+"/"+secretProviderClassName]; exists {
			secretProviderClass = &val
		} else {
			if err := r.reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretProviderClassName}, secretProviderClass); err != nil {
				return fmt.Errorf("failed to get secretProviderClass %s, err: %w", secretProviderClassName, err)
			}
			spcMap[namespace+"/"+secretProviderClassName] = *secretProviderClass
		}
		// get the pod and check if the pod has a owner reference
		pod := &corev1.Pod{}
		err = r.reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: spCacheStatuses[i].Status.PodName}, pod)
		if err != nil {
			return fmt.Errorf("CACHE failed to fetch pod during patching, err: %w", err)
		}
		var ownerRefs []metav1.OwnerReference
		for _, ownerRef := range pod.GetOwnerReferences() {
			ownerRefs = append(ownerRefs, metav1.OwnerReference{
				APIVersion: ownerRef.APIVersion,
				Kind:       ownerRef.Kind,
				UID:        ownerRef.UID,
				Name:       ownerRef.Name,
			})
		}
		// If a pod has no owner references, then it's a static pod and
		// doesn't belong to a replicaset. In this case, use the spCache as
		// owner reference just like we do it today
		if len(ownerRefs) == 0 {
			// Create a new owner ref.
			gvk, err := apiutil.GVKForObject(&spCacheStatuses[i], r.scheme)
			if err != nil {
				return err
			}
			ref := metav1.OwnerReference{
				APIVersion: gvk.GroupVersion().String(),
				Kind:       gvk.Kind,
				UID:        spCacheStatuses[i].GetUID(),
				Name:       spCacheStatuses[i].GetName(),
			}
			ownerRefs = append(ownerRefs, ref)
		}
		/*
		for _, secret := range spCache.Spec.SecretObjects {
			key := types.NamespacedName{Name: secret.SecretName, Namespace: namespace}
			val, exists := secretOwnerMap[key]
			if exists {
				secretOwnerMap[key] = append(val, ownerRefs...)
			} else {
				secretOwnerMap[key] = ownerRefs
			}
		}
		*/
	}
	/*
	for secret, owners := range secretOwnerMap {
		patchFn := func() (bool, error) {
			if err := r.patchSecretWithOwnerRef(ctx, secret.Name, secret.Namespace, owners...); err != nil {
				if !apierrors.IsConflict(err) || !apierrors.IsTimeout(err) {
					klog.ErrorS(err, "failed to set owner ref for secret", "secret", klog.ObjectRef{Namespace: secret.Namespace, Name: secret.Name})
				}
				// syncSecret.enabled is set to false by default in the helm chart for installing the driver in v0.0.23+
				// that would result in a forbidden error, so generate a warning that can be helpful for debugging
				if apierrors.IsForbidden(err) {
					klog.Warning(SyncSecretForbiddenWarning)
				}
				return false, nil
			}
			return true, nil
		}
		if err := wait.ExponentialBackoff(wait.Backoff{
			Steps:    5,
			Duration: 1 * time.Millisecond,
			Factor:   1.0,
			Jitter:   0.1,
		}, patchFn); err != nil {
			return err
		}
	}
	*/
	klog.Infof("CACHE - patcher completed")
	return nil
}

// ListOptionsLabelSelector returns a ListOptions with a label selector for node name.
func (r *SecretProviderCacheReconciler) ListOptionsLabelSelector() client.ListOption {
	return client.MatchingLabels(map[string]string{
		secretsstorev1.InternalNodeLabel: r.nodeID,
	})
}

// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretprovidercaches,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=secrets-store.csi.x-k8s.io,resources=secretprovidercaches/status,verbs=get;update;patch
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
	r.mutex.Lock()
	defer r.mutex.Unlock()

	klog.InfoS("CACHE reconcile started", "req.NamespacedName", req.NamespacedName.String())
	klog.InfoS("CACHE reconcile namespace and name", "Namespace", req.Namespace, "Name", req.Name)

	spCache := &secretsstorev1.SecretProviderCache{}
	if err := r.reader.Get(ctx, req.NamespacedName, spCache); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("CACHE - reconcile complete", "spCache", req.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "failed to get CACHE:", "spCache", req.NamespacedName.String())
		return ctrl.Result{}, err
	}

	klog.InfoS("CACHE reconcile cache", "cache", spCache)
	// Obtain the full pod metadata. An object reference is needed for sending
	// events and the UID is helpful for validating the SPCPS TargetPath.
	pod := &corev1.Pod{}
	if err := r.reader.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: spCache.Status.PodName}, pod); err != nil {
		klog.ErrorS(err, "CACHE - failed to get pod:", "pod", klog.ObjectRef{Namespace: req.Namespace, Name: spCache.Status.PodName})
		if apierrors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}
	// skip reconcile if the pod is being terminated
	// or the pod is in succeeded state (for jobs that complete aren't gc yet)
	// or the pod is in a failed state (all containers get terminated)
	if !pod.GetDeletionTimestamp().IsZero() || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		klog.V(5).InfoS("pod is being terminated, skipping reconcile", "pod", klog.KObj(pod))
		return ctrl.Result{}, nil
	}

	secretProviderClassName := spCache.Status.SecretProviderClassName
	secretProviderClass := &secretsstorev1.SecretProviderClass{}
	if err := r.reader.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: secretProviderClassName}, secretProviderClass); err != nil {
		klog.ErrorS(err, "failed to get secretProviderClass", "secretProviderClass", secretProviderClassName)
		if apierrors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}
	/*
	if len(spCache.Spec.SecretObjects) == 0 {
		klog.InfoS("no secret objects defined for spCache, nothing to reconcile", "spCache", klog.KObj(spCache), "spCache", klog.KObj(spcPodStatus))
		return ctrl.Result{}, nil
	}

	// determine which pod volume this is associated with
	podVol := k8sutil.SPCVolume(pod, spCache.Name)
	if podVol == nil {
		return ctrl.Result{}, fmt.Errorf("failed to find secret provider class pod status volume for pod %s/%s", req.Namespace, spcPodStatus.Status.PodName)
	}

	// validate TargetPath
	if fileutil.GetPodUIDFromTargetPath(spcPodStatus.Status.TargetPath) != string(pod.UID) {
		return ctrl.Result{}, fmt.Errorf("secret provider class pod status targetPath did not match pod UID for pod %s/%s", req.Namespace, spcPodStatus.Status.PodName)
	}
	if fileutil.GetVolumeNameFromTargetPath(spcPodStatus.Status.TargetPath) != podVol.Name {
		return ctrl.Result{}, fmt.Errorf("secret provider class pod status volume name did not match pod Volume for pod %s/%s", req.Namespace, spcPodStatus.Status.PodName)
	}

	files, err := fileutil.GetMountedFiles(spcPodStatus.Status.TargetPath)
	if err != nil {
		r.generateEvent(pod, corev1.EventTypeWarning, secretCreationFailedReason, fmt.Sprintf("failed to get mounted files, err: %+v", err))
		klog.ErrorS(err, "failed to get mounted files", "spCache", klog.KObj(spCache), "pod", klog.KObj(pod), "spCache", klog.KObj(spcPodStatus))
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}
	errs := make([]error, 0)
	for _, secretObj := range spCache.Spec.SecretObjects {
		secretName := strings.TrimSpace(secretObj.SecretName)

		if err = secretutil.ValidateSecretObject(*secretObj); err != nil {
			klog.ErrorS(err, "failed to validate secret object in spCache", "spCache", klog.KObj(spCache), "pod", klog.KObj(pod), "spCache", klog.KObj(spcPodStatus))
			errs = append(errs, fmt.Errorf("failed to validate secret object in spCache %s/%s, err: %w", spCache.Namespace, spCache.Name, err))
			continue
		}
		exists, err := r.secretExists(ctx, secretName, req.Namespace)
		if err != nil {
			klog.ErrorS(err, "failed to check if secret exists", "secret", klog.ObjectRef{Namespace: req.Namespace, Name: secretName}, "spCache", klog.KObj(spCache), "pod", klog.KObj(pod), "spCache", klog.KObj(spcPodStatus))
			// syncSecret.enabled is set to false by default in the helm chart for installing the driver in v0.0.23+
			// that would result in a forbidden error, so generate a warning that can be helpful for debugging
			if apierrors.IsForbidden(err) {
				klog.Warning(SyncSecretForbiddenWarning)
			}
			errs = append(errs, fmt.Errorf("failed to check if secret %s exists, err: %w", secretName, err))
			continue
		}

		var funcs []func() (bool, error)

		if !exists {
			secretType := secretutil.GetSecretType(strings.TrimSpace(secretObj.Type))

			var datamap map[string][]byte
			if datamap, err = secretutil.GetSecretData(secretObj.Data, secretType, files); err != nil {
				r.generateEvent(pod, corev1.EventTypeWarning, secretCreationFailedReason, fmt.Sprintf("failed to get data in spCache %s/%s for secret %s, err: %+v", req.Namespace, spCacheName, secretName, err))
				klog.ErrorS(err, "failed to get data in spCache for secret", "spCache", klog.KObj(spCache), "pod", klog.KObj(pod), "secret", klog.ObjectRef{Namespace: req.Namespace, Name: secretName}, "spCache", klog.KObj(spcPodStatus))
				errs = append(errs, fmt.Errorf("failed to get data in spCache %s/%s for secret %s, err: %w", req.Namespace, spCacheName, secretName, err))
				continue
			}

			labelsMap := make(map[string]string)
			if secretObj.Labels != nil {
				labelsMap = secretObj.Labels
			}
			annotationsMap := make(map[string]string)
			if secretObj.Annotations != nil {
				annotationsMap = secretObj.Annotations
			}
			// Set secrets-store.csi.k8s.io/managed=true label on the secret that's created and managed
			// by the secrets-store-csi-driver. This label will be used to perform a filtered list watch
			// only on secrets created and managed by the driver
			labelsMap[SecretManagedLabel] = "true"

			createFn := func() (bool, error) {
				if err := r.createK8sSecret(ctx, secretName, req.Namespace, datamap, labelsMap, annotationsMap, secretType); err != nil {
					klog.ErrorS(err, "failed to create Kubernetes secret", "spCache", klog.KObj(spCache), "pod", klog.KObj(pod), "secret", klog.ObjectRef{Namespace: req.Namespace, Name: secretName}, "spCache", klog.KObj(spcPodStatus))
					// syncSecret.enabled is set to false by default in the helm chart for installing the driver in v0.0.23+
					// that would result in a forbidden error, so generate a warning that can be helpful for debugging
					if apierrors.IsForbidden(err) {
						klog.Warning(SyncSecretForbiddenWarning)
					}
					return false, nil
				}
				return true, nil
			}
			funcs = append(funcs, createFn)
		}

		for _, f := range funcs {
			if err := wait.ExponentialBackoff(wait.Backoff{
				Steps:    5,
				Duration: 1 * time.Millisecond,
				Factor:   1.0,
				Jitter:   0.1,
			}, f); err != nil {
				r.generateEvent(pod, corev1.EventTypeWarning, secretCreationFailedReason, err.Error())
				return ctrl.Result{RequeueAfter: 5 * time.Second}, err
			}
		}
	}

	if len(errs) > 0 {
		return ctrl.Result{Requeue: true}, nil
	}*/

	klog.InfoS("reconcile complete", "spCache", klog.KObj(spCache), "pod", klog.KObj(pod), "secretProviderClass", klog.KObj(secretProviderClass))
	// requeue the spCache pod status again after 5mins to check if secret and ownerRef exists
	// and haven't been modified. If secret doesn't exist, then this requeue will ensure it's
	// created in the next reconcile and the owner ref patched again
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
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
// internal.secrets-store.csi.k8s.io/node-name: <node name> label. If belongs to node, then the spCache is processed.
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