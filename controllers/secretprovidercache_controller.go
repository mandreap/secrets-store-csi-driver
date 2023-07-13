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
	"fmt"
	"strings"
	"sync"
	"time"

	secretsstorev1 "sigs.k8s.io/secrets-store-csi-driver/apis/v1"
	"sigs.k8s.io/secrets-store-csi-driver/pkg/client/clientset/versioned/scheme"
	"sigs.k8s.io/secrets-store-csi-driver/pkg/util/secretutil"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (r *SecretProviderCacheReconciler) RunPatcher(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Patcher(ctx); err != nil {
				klog.ErrorS(err, "failed to patch secrets")
			}
		}
	}
}

func getSecretObjectsFromPodAndSPC(pod *corev1.Pod, spc *secretsstorev1.SecretProviderClass) []secretsstorev1.SecretObject {
	var podSpcSecretsList []secretsstorev1.SecretObject
	if spc == nil || pod == nil {
		klog.Info("secret provider class or pod is nil")
		return podSpcSecretsList
	}

	var secretList []secretsstorev1.SecretObject
	// get the secret objects from the pod
	for _, v := range pod.Spec.Volumes {
		if v.CSI != nil {
			secretList = append(secretList, secretsstorev1.SecretObject{
				SecretName: v.Name,
			})
		}
	}

	// get the secret objects from the secret provider class
	for _, v := range spc.Spec.SecretObjects {
		for _, s := range secretList {
			if s.SecretName == v.SecretName {
				s.Annotations = v.Annotations
				s.Labels = v.Labels
				klog.InfoS("secret list object", "s.Data", s.Data, "s.Type", s.Type)
				klog.InfoS("spc.Spec.SecretObjects object", "v.Data", v.Data, "v.Type", v.Type)
				s.Data = v.Data
				s.Type = v.Type
			}
			podSpcSecretsList = append(podSpcSecretsList, s)
		}
	}

	klog.InfoS("podSpcSecretsList", "podSpcSecretsList", podSpcSecretsList)
	return podSpcSecretsList
}

func (r *SecretProviderCacheReconciler) Patcher(ctx context.Context) error {
	klog.V(10).Info("patcher started")
	r.mutex.Lock()
	defer r.mutex.Unlock()

	spcCacheList := &secretsstorev1.SecretProviderCacheList{}
	spcMap := make(map[string]secretsstorev1.SecretProviderClass)
	//secretOwnerMap := make(map[types.NamespacedName][]metav1.OwnerReference)
	// get a list of all spc pod status that belong to the node
	err := r.reader.List(ctx, spcCacheList, r.ListOptionsLabelSelector())
	if err != nil {
		return fmt.Errorf("failed to list secret provider cache, err: %w", err)
	}

	spcCaches := spcCacheList.Items
	for i, spcCache := range spcCaches {
		needsUpdate := false
		for j, mapping := range spcCache.Status.SPCaPodSpcSecretsMapping {
			klog.InfoS("CACHEP context mapping", "j", j, "spcCaches[i].Status.SPCaPodSpcSecretsMapping", mapping)
			spcName := mapping.SecretProviderClassName
			spc := &secretsstorev1.SecretProviderClass{}
			namespace := spcCaches[i].Namespace

			if val, exists := spcMap[namespace+"/"+spcName]; exists {
				spc = &val
			} else {
				if err := r.reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: spcName}, spc); err != nil {
					return fmt.Errorf("CACHEP failed to get spc %s, err: %w", spcName, err)
				}
				spcMap[namespace+"/"+spcName] = *spc
			}
			// get the pod and check if the pod has a owner reference
			pod := &corev1.Pod{}
			err = r.reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: mapping.PodName}, pod)
			if err != nil {
				return fmt.Errorf("failed to fetch pod during patching, err: %w", err)
			}
			
			secrets := getSecretObjectsFromPodAndSPC(pod, spc)
			if len(secrets) > 0 && len(mapping.SecretObjects) == 0 {
				for _, secret := range secrets {
					mapping.SecretObjects = append(mapping.SecretObjects, &secret)
					klog.Info("CACHEP adding mapping secrets", "secret", secret)
				}
				klog.Info("CACHEP replacing mapping secrets", "mapping.SecretObjects", mapping.SecretObjects)
				needsUpdate = true
			}

			// TODO: take the real secret data here instead of the synced secret key and value
			for _, secret := range mapping.SecretObjects {
				for _, s := range secrets {
					klog.Info("CACHEP updating secret", "secret", secret, "s", s)
					if secret.SecretName == s.SecretName {
						needsUpdate = true
						secret.Annotations = s.Annotations
						secret.Labels = s.Labels
						secret.Data = s.Data
						secret.Type = s.Type
					}
				}
			}
		}
		if needsUpdate {
			klog.Info("CACHEP updating spc", "spcCache", spcCache)
			patch := client.MergeFromWithOptions(spcCache.DeepCopy(), client.MergeFromWithOptimisticLock{})
			if err := r.writer.Patch(ctx, &spcCache, patch); err != nil {
				return fmt.Errorf("failed to patch spc, err: %w", err)
			}
		}
	}

	klog.V(10).Info("patcher completed")
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

func (r *SecretProviderCacheReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	klog.InfoS("CACHE reconcile started", "spc", req.NamespacedName.String())
	klog.InfoS("CACHE context", "context", ctx)
	klog.InfoS("CACHE request", "req", req)
	spcCache := &secretsstorev1.SecretProviderCache{}
	if err := r.reader.Get(ctx, req.NamespacedName, spcCache); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("CACHE reconcile complete", "spc", req.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "failed to get spcCache", "spcCache", req.NamespacedName.String())
		return ctrl.Result{}, err
	}

	klog.InfoS("CACHE reconcile CACHE", "spcCache", spcCache)
	// Obtain the full pod metadata. An object reference is needed for sending
	// events and the UID is helpful for validating the SPCPS TargetPath.
	for _, mapping := range spcCache.Status.SPCaPodSpcSecretsMapping {
		pod := &corev1.Pod{}
		if err := r.reader.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: mapping.PodName}, pod); err != nil {
			klog.ErrorS(err, "failed to get pod", "pod", klog.ObjectRef{Namespace: req.Namespace, Name: mapping.PodName})
			if apierrors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			return ctrl.Result{}, err
		}

		klog.InfoS("CACHE reconcile pod", "pod", pod.Name)

		// skip reconcile if the pod is being terminated
		// or the pod is in succeeded state (for jobs that complete aren't gc yet)
		// or the pod is in a failed state (all containers get terminated)
		if !pod.GetDeletionTimestamp().IsZero() || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			klog.V(5).InfoS("pod is being terminated, skipping reconcile", "pod", klog.KObj(pod))
			return ctrl.Result{}, nil
		}

		spcName := mapping.SecretProviderClassName
		spc := &secretsstorev1.SecretProviderClass{}
		if err := r.reader.Get(ctx, client.ObjectKey{Namespace: req.Namespace, Name: spcName}, spc); err != nil {
			klog.ErrorS(err, "failed to get spc", "spc", spcName)
			if apierrors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			return ctrl.Result{}, err
		}

		if len(spc.Spec.SecretObjects) == 0 {
			klog.InfoS("no secret objects defined for spc, nothing to reconcile", "spc", klog.KObj(spc), "spcps", klog.KObj(spcCache))
			return ctrl.Result{}, nil
		}

		errs := make([]error, 0)
		for _, secretObj := range spc.Spec.SecretObjects {
			secretName := strings.TrimSpace(secretObj.SecretName)

			if err := secretutil.ValidateSecretObject(*secretObj); err != nil {
				klog.ErrorS(err, "failed to validate secret object in spc", "spc", klog.KObj(spc), "pod", klog.KObj(pod), "spcps", klog.KObj(spcCache))
				errs = append(errs, fmt.Errorf("failed to validate secret object in spc %s/%s, err: %w", spc.Namespace, spc.Name, err))
				continue
			}
			klog.InfoS("Checking secret exists:", "secretName", secretName, "mapping.SecretObject", mapping.SecretObjects)
			exists, err := r.secretExists(ctx, secretName, mapping.SecretObjects)
			if err != nil {
				klog.ErrorS(err, "failed to check if secret exists in the cache", "secret", secretName, "spc", klog.KObj(spc), "pod", klog.KObj(pod), "spcps", klog.KObj(spcCache))
				errs = append(errs, fmt.Errorf("failed to check if secret %s exists, err: %w", secretName, err))
				continue
			}
			
			if !exists {
				klog.InfoS("secret doesn't exist in the cache, creating", "secret", secretName, "spc", klog.KObj(spc), "pod", klog.KObj(pod), "spcps", klog.KObj(spcCache))	
				mapping.SecretObjects = append(mapping.SecretObjects, secretObj)
			}
		}

		if len(errs) > 0 {
			return ctrl.Result{Requeue: true}, nil
		}
		klog.InfoS("reconcile complete", "spc", klog.KObj(spc), "pod", klog.KObj(pod), "spcps", klog.KObj(spcCache))
	}
	

	// requeue the spc pod status again after 5mins to check if secret and ownerRef exists
	// and haven't been modified. If secret doesn't exist, then this requeue will ensure it's
	// created in the next reconcile and the owner ref patched again
	return ctrl.Result{}, nil
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


// TODO:: this should be patched with SA and secret-provider class
// Once the SA disappears, presumably the secret-provider classes federated with this SA will
// disappear as well 
// Am I missing something for the Service Principal?
// patchSecretWithOwnerRef patches the secret owner reference with the spc pod status
func (r *SecretProviderCacheReconciler) patchSecretWithOwnerRef(ctx context.Context, name, namespace string, ownerRefs ...metav1.OwnerReference) error {
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	if err := r.Client.Get(ctx, secretKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(5).InfoS("secret not found for patching", "secret", klog.ObjectRef{Namespace: namespace, Name: name})
			return nil
		}
		return err
	}

	patch := client.MergeFromWithOptions(secret.DeepCopy(), client.MergeFromWithOptimisticLock{})
	needsPatch := false

	secretOwnerRefs := secret.GetOwnerReferences()
	secretOwnerMap := make(map[string]types.UID)
	for _, or := range secretOwnerRefs {
		secretOwnerMap[or.Name] = or.UID
	}

	for i := range ownerRefs {
		if _, exists := secretOwnerMap[ownerRefs[i].Name]; exists {
			continue
		}
		// add to map for tracking
		secretOwnerMap[ownerRefs[i].Name] = ownerRefs[i].UID
		needsPatch = true
		klog.V(5).InfoS("Adding owner ref for secret", "ownerRefAPIVersion", ownerRefs[i].APIVersion, "ownerRefName", ownerRefs[i].Name, "secret", klog.ObjectRef{Namespace: namespace, Name: name})
		secretOwnerRefs = append(secretOwnerRefs, ownerRefs[i])
	}

	if needsPatch {
		secret.SetOwnerReferences(secretOwnerRefs)
		return r.writer.Patch(ctx, secret, patch)
	}
	return nil
}


// Look into the CRD and check if we already have a sp:secret mapping and patch the secret accordingly
// secretExists checks if the secret with name already exists
func (r *SecretProviderCacheReconciler) secretExists(ctx context.Context, name string, listOfSecrets []*secretsstorev1.SecretObject) (bool, error) {
	klog.InfoS("Check if secret exists", "secretName", name)
	
	if len(listOfSecrets) == 0 {
		return false, nil
	}

	for _, secret := range listOfSecrets {
		if secret.SecretName == name {
			klog.InfoS("Check if name matches and they do", "name", name, "secretName:", secret.SecretName)
			return true, nil
		}
	}
	klog.InfoS("Couldn't find secret", "secret name:", name)
	return false, nil
}

// generateEvent generates an event
func (r *SecretProviderCacheReconciler) generateEvent(obj apiruntime.Object, eventType, reason, message string) {
	if obj != nil {
		r.eventRecorder.Eventf(obj, eventType, reason, message)
	}
}
