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

package secretsstore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	secretsstorev1 "sigs.k8s.io/secrets-store-csi-driver/apis/v1"
	"sigs.k8s.io/secrets-store-csi-driver/pkg/util/runtimeutil"
	"sigs.k8s.io/secrets-store-csi-driver/pkg/util/spcpsutil"
	"sigs.k8s.io/secrets-store-csi-driver/provider/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ensureMountPoint ensures mount point is valid
func (ns *nodeServer) ensureMountPoint(target string) (bool, error) {
	notMnt, err := ns.mounter.IsLikelyNotMountPoint(target)
	if err != nil {
		return !notMnt, err
	}

	if !notMnt {
		// testing original mount point, make sure the mount link is valid
		_, err := os.ReadDir(target)
		if err == nil {
			klog.InfoS("already mounted to target", "targetPath", target)
			// already mounted
			return !notMnt, nil
		}
		if err := ns.mounter.Unmount(target); err != nil {
			klog.ErrorS(err, "failed to unmount directory", "targetPath", target)
			return !notMnt, err
		}
		notMnt = true
		// remount it in node publish
		return !notMnt, err
	}

	if runtimeutil.IsRuntimeWindows() {
		// IsLikelyNotMountPoint always returns notMnt=true for windows as the
		// target path is not a soft link to the global mount
		// instead check if the dir exists for windows and if it's not empty
		// If there are contents in the dir, then objects are already mounted
		f, err := os.ReadDir(target)
		if err != nil {
			return !notMnt, err
		}
		if len(f) > 0 {
			notMnt = false
			return !notMnt, err
		}
	}

	return false, nil
}

// getSecretProviderItem returns the secretproviderclass object by name and namespace
func getSecretProviderItem(ctx context.Context, c client.Client, name, namespace string) (*secretsstorev1.SecretProviderClass, error) {
	spc := &secretsstorev1.SecretProviderClass{}
	spcKey := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	if err := c.Get(ctx, spcKey, spc); err != nil {
		return nil, fmt.Errorf("failed to get secretproviderclass %s/%s, error: %w", namespace, name, err)
	}
	return spc, nil
}

func addOrUpdateCacheObjectsVersion(cacheObjectVersionsSlice []*secretsstorev1.CacheObjectVersions, objectVersions []*v1alpha1.ObjectVersion) (bool, []*secretsstorev1.CacheObjectVersions) {
	klog.InfoS("addOrUpdateCacheObjectsVersion", "cacheObjectVersionsSlice", cacheObjectVersionsSlice, "objectVersions", objectVersions)
	if len(objectVersions) == 0 && len(cacheObjectVersionsSlice) == 0 {
		return false, cacheObjectVersionsSlice
	}

	if len(objectVersions) > 0 && len(cacheObjectVersionsSlice) == 0 {
		for _, objectVersion := range objectVersions {
			cacheObjectVersionsSlice = append(cacheObjectVersionsSlice, &secretsstorev1.CacheObjectVersions{
				Id:      objectVersion.Id,
				Version: objectVersion.Version,
			})
		}
		klog.InfoS("addOrUpdateCacheObjectsVersion shouldUpdate=true", "cacheObjectVersionsSlice", cacheObjectVersionsSlice)
		return true, cacheObjectVersionsSlice
	}

	// TODO: can make this more efficient
	shouldUpdate := false
	for _, objectVersion := range objectVersions {
		foundObjectVersion := false
		for _, cacheObjectVersion := range cacheObjectVersionsSlice {
			if objectVersion.Id == cacheObjectVersion.Id && objectVersion.Version == cacheObjectVersion.Version {
				foundObjectVersion = true
				break
			}
			if objectVersion.Id == cacheObjectVersion.Id && objectVersion.Version != cacheObjectVersion.Version {
				cacheObjectVersion.Version = objectVersion.Version
				foundObjectVersion = true
				shouldUpdate = true
				break
			}
		}
		if !foundObjectVersion {
			cacheObjectVersionsSlice = append(cacheObjectVersionsSlice, &secretsstorev1.CacheObjectVersions{
				Id:      objectVersion.Id,
				Version: objectVersion.Version,
			})
			shouldUpdate = true
		}
	}

	klog.InfoS("addOrUpdateCacheObjectsVersion", "shouldUpdate", shouldUpdate, "cacheObjectVersionsSlice", cacheObjectVersionsSlice)
	return shouldUpdate, cacheObjectVersionsSlice
}

func addOrUpdateCacheFiles(cacheFileSlice []*secretsstorev1.CacheFile, fileSecrets []*v1alpha1.File) (bool, []*secretsstorev1.CacheFile) {
	var shouldUpdate bool = false
	for _, fileSecret := range fileSecrets {
		var foundFile *secretsstorev1.CacheFile = nil
		for _, f := range cacheFileSlice {
			if fileSecret.Path == f.Path {
				foundFile = f
				break
			}
		}

		if foundFile == nil {
			// add the cacheFileSlice to the cache
			cacheFileSlice = append(cacheFileSlice, &secretsstorev1.CacheFile{
				Path:     fileSecret.Path,
				Mode:     fileSecret.Mode,
				Contents: fileSecret.Contents,
			})
			shouldUpdate = true
			break
		}

		if foundFile.Mode != fileSecret.Mode {
			foundFile.Mode = fileSecret.Mode
			shouldUpdate = true
		}

		if !bytes.Equal(foundFile.Contents, fileSecret.Contents) {
			shouldUpdate = true
		}
	}
	klog.InfoS("addOrUpdateCacheFiles", "shouldUpdate", shouldUpdate, "cacheFileSlice", cacheFileSlice)
	return shouldUpdate, cacheFileSlice
}

func addObjectVersionsToCacheObjectVersions(cacheObjectVersionsSlice []*secretsstorev1.CacheObjectVersions, objectVersions []*v1alpha1.ObjectVersion) []*secretsstorev1.CacheObjectVersions {
	for _, objectVersion := range objectVersions {
		cacheObjectVersion := &secretsstorev1.CacheObjectVersions{
			Id:      objectVersion.Id,
			Version: objectVersion.Version,
		}
		cacheObjectVersionsSlice = append(cacheObjectVersionsSlice, cacheObjectVersion)
	}
	klog.InfoS("addObjectVersionsToCacheObjectVersions", "cacheObjectVersionsSlice", cacheObjectVersionsSlice)
	return cacheObjectVersionsSlice
}

func addFileSecretsToCacheFile(cacheFileSlice []*secretsstorev1.CacheFile, fileSecrets []*v1alpha1.File) []*secretsstorev1.CacheFile {
	for _, file := range fileSecrets {
		cacheFile := &secretsstorev1.CacheFile{
			Path:     file.Path,
			Mode:     file.Mode,
			Contents: file.Contents,
		}

		cacheFileSlice = append(cacheFileSlice, cacheFile)
	}
	klog.InfoS("addFileSecretsToCacheFile", "cacheFileSlice", cacheFileSlice)
	return cacheFileSlice
}

// createOrUpdateSecretProviderCache creates secret provider cache if it doesn't exist.
// if the secret provider cache already exists, it updates the status and owner references.
func createOrUpdateSecretProviderCache(ctx context.Context, c client.Client, reader client.Reader, serviceAccountName, podName, namespace, spcName, nodeID, nodeRefKey string, fileSecrets []*v1alpha1.File, objectVersions []*v1alpha1.ObjectVersion, cacheEncryptionKey *corev1.Secret) error {
	// todo: this should be UID
	nodeRef := nodeRefKey
	if nodeRef == "" {
		nodeRef = "invalidnoderef"
	}
	spCacheName := namespace + spcName + serviceAccountName + nodeRef
	klog.InfoS("creating secret provider cache", "spCache", spCacheName)

	// TODO: add object versions to cache
	secretFiles := []*secretsstorev1.CacheFile{}
	secretFiles = addFileSecretsToCacheFile(secretFiles, fileSecrets)

	//TODO: can just remove this and use the cacheObjectVersionsSlice directly from the cacheSpcWorkloadFiles
	cacheObjectVersions := []*secretsstorev1.CacheObjectVersions{}
	cacheObjectVersions = addObjectVersionsToCacheObjectVersions(cacheObjectVersions, objectVersions)

	// retrieve the pod and try to get its owners
	pod := &corev1.Pod{}
	err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod)
	if err != nil {
		klog.ErrorS(err, "failed to get pod", "podName", podName)
		return err
	}

	var warningNoPersistencyOnRestart bool = true
	var workloadName string = podName
	var ownerRefName string = podName
	var ownerRefKind string = "Pod"
	var ownerUID string = ""
	//TODO: what if we have n owners
	if len(pod.OwnerReferences) == 0 {
		klog.InfoS("pod doesn't have owner references:", "podName", podName)
		ownerUID = string(pod.UID)
	} else {
		for _, ownerRef := range pod.OwnerReferences {
			klog.InfoS("Pod owner references", "ownerRef", ownerRef.Name, "ownerRefUID", ownerRef.UID, "ownerRefKind", ownerRef.Kind)
			podHash, ok := pod.Labels["pod-template-hash"]
			if ownerRef.Kind != "Pod" && ok && strings.Contains(ownerRef.Name, podHash) {
				warningNoPersistencyOnRestart = false
				ownerUID = string(ownerRef.UID)
				ownerRefName = ownerRef.Name
				ownerRefKind = ownerRef.Kind
				workloadName = strings.ReplaceAll(ownerRef.Name, podHash, "")
				workloadName = strings.TrimRight(workloadName, "-")
				break
			}
		}
	}

	cacheWorkloadItem := &secretsstorev1.CacheWorkload{
		WorkloadName:       workloadName,
		OwnerReferenceName: ownerRefName,
		OwnerReferenceKind: ownerRefKind,
		OwnerReferenceUID:  ownerUID,
		CachedPods:         map[string]string{podName: podName},
	}

	workloadsMap := make(map[string]*secretsstorev1.CacheWorkload)
	workloadsMap[workloadName] = cacheWorkloadItem

	cacheSpcWorkloadFiles := &secretsstorev1.CacheSpcWorkloadFiles{
		FileObjectVersions: cacheObjectVersions,
		SecretFiles:        secretFiles,
		WorkloadsMap:       workloadsMap,
	}
	klog.InfoS("cacheSpcWorkloadFiles", "cacheSpcWorkloadFiles", cacheSpcWorkloadFiles)
	spCache := &secretsstorev1.SecretProviderCache{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spCacheName,
			Namespace: namespace,
		},
		Spec: secretsstorev1.SecretProviderCacheSpec{
			ServiceAccountName:      serviceAccountName,
			NodePublishSecretRef:    nodeRefKey,
			SpcFilesWorkloads:       cacheSpcWorkloadFiles,
			SecretProviderClassName: spcName,
		},
		Status: secretsstorev1.SecretProviderCacheStatus{
			WarningNoPersistencyOnRestart: warningNoPersistencyOnRestart,
		},
	}
	klog.InfoS("spCache before creation", "spCache", spCache)

	spc := &secretsstorev1.SecretProviderClass{}
	err = reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: spcName}, spc)
	if err != nil {
		klog.ErrorS(err, "failed to get secretproviderclass", "spcName", spcName)
		return err
	}

	// Set owner reference as the secret provider class as the mapping between secret provider cache and
	// secret provider class is 1 to 1. When secret provider class is deleted, the cache will automatically be garbage collected
	spCache.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "SecretProviderClass",
			Name:       spcName,
			UID:        types.UID(spc.UID),
		},
	})

	err = c.Create(ctx, spCache)
	if err == nil {
		klog.Info("SPCache created: %s", spCacheName)
		klog.InfoS("SecretProviderCache: successfully created", "spCache", spCache)
		spCache.Status.WarningNoPersistencyOnRestart = warningNoPersistencyOnRestart
		return c.Status().Update(ctx, spCache)
	}

	if !apierrors.IsAlreadyExists(err) {
		return err
	}

	klog.InfoS("SecretProviderCache: already exists, updating it", "cache", spCacheName)

	spCacheUpdate := &secretsstorev1.SecretProviderCache{}
	if err := c.Get(ctx, client.ObjectKey{Name: spCacheName, Namespace: namespace}, spCacheUpdate); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		// the secret provider cache could be missing in the cache because it was labeled with a different node
		// label, so we need to get it from the API server
		if err = reader.Get(ctx, client.ObjectKey{Name: spCacheName, Namespace: namespace}, spCacheUpdate); err != nil {
			return err
		}
	}

	klog.InfoS("cache encryption key", "key", string(cacheEncryptionKey.Data["key"]))
	defer func() {
		if spCacheUpdate.Status.WarningNoPersistencyOnRestart != warningNoPersistencyOnRestart && warningNoPersistencyOnRestart {
			spCacheUpdate.Status.WarningNoPersistencyOnRestart = warningNoPersistencyOnRestart
		}
	}()

	var shouldUpdateCache bool = false
	shouldUpdateCache, spCacheUpdate.Spec.SpcFilesWorkloads.SecretFiles = addOrUpdateCacheFiles(spCacheUpdate.Spec.SpcFilesWorkloads.SecretFiles, fileSecrets)

	var shouldUpdateCacheSecretVersions bool = false
	shouldUpdateCacheSecretVersions, spCacheUpdate.Spec.SpcFilesWorkloads.FileObjectVersions = addOrUpdateCacheObjectsVersion(spCacheUpdate.Spec.SpcFilesWorkloads.FileObjectVersions, objectVersions)
	if shouldUpdateCacheSecretVersions {
		shouldUpdateCache = shouldUpdateCacheSecretVersions
	}
	klog.InfoS("spCacheUpdate.Spec.SpcFilesWorkloads.FileObjectVersions", "spCacheUpdate.Spec.SpcFilesWorkloads.FileObjectVersions", spCacheUpdate.Spec.SpcFilesWorkloads.FileObjectVersions)

	// TODO: remove these 2 -> should never happen
	if serviceAccountName != spCacheUpdate.Spec.ServiceAccountName {
		klog.InfoS("ServiceAccountName is different, updating the cache:", "serviceAccountName", serviceAccountName)
		spCacheUpdate.Spec.ServiceAccountName = serviceAccountName
		shouldUpdateCache = true
	}
	if spCacheUpdate.Spec.NodePublishSecretRef != nodeRefKey {
		klog.InfoS("NodePublishSecretRef is different, updating the cache:", "nodeRefKey", nodeRefKey)
		spCacheUpdate.Spec.NodePublishSecretRef = nodeRefKey
		shouldUpdateCache = true
	}

	klog.InfoS("Check the workload is present:", "workload ", workloadName, "spCacheUpdate.Spec.SpcFilesWorkloads.WorkloadsMap", spCacheUpdate.Spec.SpcFilesWorkloads.WorkloadsMap)
	if spCacheUpdate.Spec.SpcFilesWorkloads.WorkloadsMap == nil {
		klog.InfoS("Workload map is nil, creating a new one")
		spCacheUpdate.Spec.SpcFilesWorkloads.WorkloadsMap = workloadsMap
		shouldUpdateCache = true
	} else {
		existingWorkloadItem, ok := spCacheUpdate.Spec.SpcFilesWorkloads.WorkloadsMap[workloadName]
		if !ok {
			klog.InfoS("Adding a new workload to the cache:", "workloadName", workloadName)
			spCacheUpdate.Spec.SpcFilesWorkloads.WorkloadsMap[workloadName] = cacheWorkloadItem
			shouldUpdateCache = true
		} else {

			klog.InfoS("Workload already present in the cache:", "workloadName", workloadName)
			klog.InfoS("Check pod is present", "pod = ", podName)
			if spCacheUpdate.Spec.SpcFilesWorkloads.WorkloadsMap[workloadName].CachedPods == nil {
				klog.InfoS("Pod map is nil, creating a new one")
				spCacheUpdate.Spec.SpcFilesWorkloads.WorkloadsMap[workloadName].CachedPods = map[string]string{podName: podName}
				shouldUpdateCache = true
			} else {
				_, podIsPresent := existingWorkloadItem.CachedPods[podName]
				if !podIsPresent {
					klog.InfoS("Adding a new pod to the cache:", "workloadName", workloadName, "podName", podName, "podUID", pod.UID)
					spCacheUpdate.Spec.SpcFilesWorkloads.WorkloadsMap[workloadName].CachedPods[podName] = podName
					shouldUpdateCache = true
				}
			}

			if ownerUID != existingWorkloadItem.OwnerReferenceUID {
				klog.InfoS("Different UIDs", "workloadName", workloadName, "ownerUID", ownerUID, "existing ownerUID", existingWorkloadItem.OwnerReferenceUID)
				spCacheUpdate.Spec.SpcFilesWorkloads.WorkloadsMap[workloadName].WarningNewUID = true
			}
		}
	}

	for _, ownerRef := range spCacheUpdate.OwnerReferences {
		if ownerRef.Kind == "SecretProviderClass" {
			if ownerRef.Name != spcName || ownerRef.UID != spc.UID {
				klog.InfoS("ownerRef.Name is different", "expected", spcName, "got owner", ownerRef.Name)
			}
		}
	}

	if shouldUpdateCache {
		err = c.Update(ctx, spCacheUpdate)
		if err != nil {
			return err
		}
	}

	klog.InfoS("Update final", "cache", spCacheName)
	return c.Status().Update(ctx, spCacheUpdate)
}

// createOrUpdateSecretProviderClassPodStatus creates secret provider class pod status if not exists.
// if the secret provider class pod status already exists, it'll update the status and owner references.
func createOrUpdateSecretProviderClassPodStatus(ctx context.Context, c client.Client, reader client.Reader, podname, namespace, podUID, spcName, targetPath, nodeID string, mounted bool, objects map[string]string) error {
	var o []secretsstorev1.SecretProviderClassObject
	var err error
	spcpsName := podname + "-" + namespace + "-" + spcName

	for k, v := range objects {
		o = append(o, secretsstorev1.SecretProviderClassObject{ID: k, Version: v})
	}
	o = spcpsutil.OrderSecretProviderClassObjectByID(o)

	spcPodStatus := &secretsstorev1.SecretProviderClassPodStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spcpsName,
			Namespace: namespace,
			Labels:    map[string]string{secretsstorev1.InternalNodeLabel: nodeID},
		},
		Status: secretsstorev1.SecretProviderClassPodStatusStatus{
			PodName:                 podname,
			TargetPath:              targetPath,
			Mounted:                 mounted,
			SecretProviderClassName: spcName,
			Objects:                 o,
		},
	}

	// Set owner reference to the pod as the mapping between secret provider class pod status and
	// pod is 1 to 1. When pod is deleted, the spc pod status will automatically be garbage collected
	spcPodStatus.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "Pod",
			Name:       podname,
			UID:        types.UID(podUID),
		},
	})

	if err = c.Create(ctx, spcPodStatus); err == nil || !apierrors.IsAlreadyExists(err) {
		return err
	}
	klog.Info("SecretProviderClassPodStatus: successfully created")
	klog.InfoS("secret provider class pod status already exists, updating it", "spcps", klog.ObjectRef{Name: spcPodStatus.Name, Namespace: spcPodStatus.Namespace})

	spcps := &secretsstorev1.SecretProviderClassPodStatus{}
	// the secret provider class pod status with the name already exists, update it
	if err = c.Get(ctx, client.ObjectKey{Name: spcpsName, Namespace: namespace}, spcps); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		// the secret provider class pod status could be missing in the cache because it was labeled with a different node
		// label, so we need to get it from the API server
		if err = reader.Get(ctx, client.ObjectKey{Name: spcpsName, Namespace: namespace}, spcps); err != nil {
			return err
		}
	}
	// update the labels of the secret provider class pod status to match the node label
	spcps.Labels[secretsstorev1.InternalNodeLabel] = nodeID
	spcps.Status = spcPodStatus.Status
	spcps.OwnerReferences = spcPodStatus.OwnerReferences

	return c.Update(ctx, spcps)
}

// getProviderFromSPC returns the provider as defined in SecretProviderClass
func getProviderFromSPC(spc *secretsstorev1.SecretProviderClass) (string, error) {
	if len(spc.Spec.Provider) == 0 {
		return "", fmt.Errorf("provider not set in %s/%s", spc.Namespace, spc.Name)
	}
	return string(spc.Spec.Provider), nil
}

// getParametersFromSPC returns the parameters map as defined in SecretProviderClass
func getParametersFromSPC(spc *secretsstorev1.SecretProviderClass) (map[string]string, error) {
	if len(spc.Spec.Parameters) == 0 {
		return nil, fmt.Errorf("parameters not set in %s/%s", spc.Namespace, spc.Name)
	}
	return spc.Spec.Parameters, nil
}

func getSecrets(spc *secretsstorev1.SecretProviderClass) ([]*secretsstorev1.SecretObject, error) {
	return spc.Spec.SecretObjects, nil
}

// isMockProvider returns true if the provider is mock
func isMockProvider(provider string) bool {
	return strings.EqualFold(provider, "mock_provider")
}

// isMockTargetPath returns true if the target path is mock
func isMockTargetPath(targetPath string) bool {
	return strings.EqualFold(targetPath, "/tmp/csi/mount")
}
