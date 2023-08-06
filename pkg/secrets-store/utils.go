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
	"context"
	"fmt"
	"os"
	"strings"

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

func addMissingSecretsFiles(file *[]*secretsstorev1.CacheFile, fileSecrets []*v1alpha1.File) {
	if file == nil {
		klog.InfoS("file is nil: can't add file secrets to cacheFile")
		return
	}

	for _, fileSecret := range fileSecrets {
		found := false
		for _, f := range *file {
			if fileSecret.Path == f.Path {
				found = true
				break
			}
		}
		if !found {
			*file = append(*file, &secretsstorev1.CacheFile{
				Path:     fileSecret.Path,
				Mode:     fileSecret.Mode,
				Contents: fileSecret.Contents,
			})
		}
	}
}

func addFileSecretsToCacheFile(cacheFile *[]*secretsstorev1.CacheFile, fileSecrets []*v1alpha1.File) {
	if cacheFile == nil {
		klog.InfoS("cacheFile is nil: can't add file secrets to cacheFile")
		return
	}
	for _, file := range fileSecrets {
		(*cacheFile) = append(*cacheFile, &secretsstorev1.CacheFile{
			Path:     file.Path,
			Mode:     file.Mode,
			Contents: file.Contents,
		})
	}
}

// createOrUpdateSecretProviderCache creates secret provider cache if it doesn't exist.
// if the secret provider cache already exists, it updates the status and owner references.
func createOrUpdateSecretProviderCache(ctx context.Context, c client.Client, reader client.Reader, serviceAccountName, podName, namespace, spcName, nodeID, nodeRefKey string, fileSecrets []*v1alpha1.File) error {
	spCacheName := namespace + "-" + serviceAccountName + "-cache"
	klog.InfoS("creating secret provider cache", "spCache", spCacheName)

	// map between pod name (for now) and spcSecretsMapping
	// key == podname, value == spc:secrets map
	podSecretsMap := make(map[string]*secretsstorev1.CachePodSecrets)

	// map between the spc name and the cacheSecretsMap
	// key == spc name, value == file secrets
	spcNodeRefMapping := make(map[string]*secretsstorev1.CacheSpcNodeRefMap)

	spcNodeRefMapping[spcName] = &secretsstorev1.CacheSpcNodeRefMap{
		SecretFiles:          &[]*secretsstorev1.CacheFile{},
		NodePublishSecretRef: nodeRefKey,
	}

	addFileSecretsToCacheFile(spcNodeRefMapping[spcName].SecretFiles, fileSecrets)

	// add the spcSecretsMapping to the podSecretsMap
	podSecretsMap[podName] = &secretsstorev1.CachePodSecrets{
		CacheSecretFilesSpcMapping: spcNodeRefMapping,
	}

	// add the podSecretsMap to the spCache
	spCache := &secretsstorev1.SecretProviderCache{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spCacheName,
			Namespace: namespace,
			Labels:    map[string]string{secretsstorev1.InternalNodeLabel: nodeID},
		},
		Spec: secretsstorev1.SecretProviderCacheSpec{
			// Secrets is a list of secrets that should be cached
			ServiceAccountName: serviceAccountName,
			PodSecretsMap:      podSecretsMap,
		},
	}

	// create the secret provider cache
	if err := c.Create(ctx, spCache); err == nil || !apierrors.IsAlreadyExists(err) {
		if err != nil && !apierrors.IsAlreadyExists(err) {
			klog.ErrorS(err, "failed to create secret provider cache", "spCache", spCacheName)
		}
		return err
	}

	klog.Info("SecretProviderCache: already exists, updating it")
	klog.InfoS("updating secret provider cache", "spCache", spCacheName, "secretFiles", fileSecrets)
	spCacheUpdate := &secretsstorev1.SecretProviderCache{}

	// the secret provider class pod status with the name already exists, update it
	if err := c.Get(ctx, client.ObjectKey{Name: spCacheName, Namespace: namespace}, spCacheUpdate); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		// the secret provider class pod status could be missing in the cache because it was labeled with a different node
		// label, so we need to get it from the API server
		if err = reader.Get(ctx, client.ObjectKey{Name: spCacheName, Namespace: namespace}, spCacheUpdate); err != nil {
			return err
		}
	}

	// retrieve the podSecretsMap from the spCache for the podName
	podSecretsMapUpdate, ok := spCacheUpdate.Spec.PodSecretsMap[podName]

	// if the pod wasn't found, we need to add it to the cache
	if !ok {
		// add the podSecretsMap to the spCache
		spCacheUpdate.Spec.PodSecretsMap[podName] = spCache.Spec.PodSecretsMap[podName]
		spCacheUpdate.Spec.ServiceAccountName = serviceAccountName
		spCacheUpdate.Labels = map[string]string{secretsstorev1.InternalNodeLabel: nodeID}
		return c.Update(ctx, spCacheUpdate)
	}

	// else the pod was found, we need to check if the secret provider class is already present
	spcNodeRefMappingUpdate, ok := podSecretsMapUpdate.CacheSecretFilesSpcMapping[spcName]

	// if the secret provider class wasn't found, we need to add it to the cache
	if !ok {
		// add the spcSecretsMapping to the podSecretsMap
		spCacheUpdate.Spec.PodSecretsMap[podName].CacheSecretFilesSpcMapping[spcName] = spcNodeRefMapping[spcName]
		spCacheUpdate.Spec.ServiceAccountName = serviceAccountName
		spCacheUpdate.Labels = map[string]string{secretsstorev1.InternalNodeLabel: nodeID}
		return c.Update(ctx, spCacheUpdate)
	}

	// else the secret provider class was found, we need to check if the secrets are already present
	addMissingSecretsFiles(spcNodeRefMappingUpdate.SecretFiles, fileSecrets)

	// update the labels of the secret provider class pod status to match the node label
	spCacheUpdate.Spec.ServiceAccountName = serviceAccountName
	spCacheUpdate.Labels = map[string]string{secretsstorev1.InternalNodeLabel: nodeID}
	//spCacheUpdate.Status = spCache.Status

	return c.Update(ctx, spCacheUpdate)
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
