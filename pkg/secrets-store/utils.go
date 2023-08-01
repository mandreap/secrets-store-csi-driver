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

// todo: add error checks
func addMissingSecretsToCache(unfoundSecrets map[string]string, cacheSecrets *[]*secretsstorev1.CacheSecret) {

	// for all the secrets which weren't found yet, add them to the cache
	for secretName, secretValue := range unfoundSecrets {
		// we didn't find the secret, we need to add it to the cache
		*cacheSecrets = append(*cacheSecrets, &secretsstorev1.CacheSecret{
			SecretName:       secretName,
			SecretType:       "Opaque",
			SecretObjectData: map[string][]byte{secretName: []byte(secretValue)},
		})
	}
}

// todo: add error checks
func addSecretProviderClassWithSecrets(cacheSecretsSpcMapping *[]*secretsstorev1.CacheSecretProviderClassSecrets, spcName string, secrets map[string]string) {
	// we didn't find the secretProvider class for the pod, we need to add it to the cache
	*cacheSecretsSpcMapping = append(*cacheSecretsSpcMapping, &secretsstorev1.CacheSecretProviderClassSecrets{
		SecretProviderClassName: spcName,
	})

	addMissingSecretsToCache(secrets, &(*cacheSecretsSpcMapping)[len(*cacheSecretsSpcMapping)-1].CacheSecrets)
}

// createOrUpdateSecretProviderCache creates secret provider cache if it doesn't exist.
// if the secret provider cache already exists, it updates the status and owner references.
func createOrUpdateSecretProviderCache(ctx context.Context, c client.Client, reader client.Reader, serviceAccountName, podName, namespace, podUID, spcName, targetPath, nodeID string, mounted bool, secrets, objects map[string]string) error {
	spCacheName := namespace + "-" + serviceAccountName + "-cache"
	klog.InfoS("creating secret provider cache", "spCache", spCacheName)

	klog.InfoS("secrets", "secrets", secrets)
	spCache := &secretsstorev1.SecretProviderCache{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spCacheName,
			Namespace: namespace,
			Labels:    map[string]string{secretsstorev1.InternalNodeLabel: nodeID},
		},
		Spec: secretsstorev1.SecretProviderCacheSpec{
			// Secrets is a list of secrets that should be cached
			SpecFoo:            "test spec value",
			ServiceAccountName: serviceAccountName,
			PodSecrets: []*secretsstorev1.CachePodSecrets{
				{
					PodName: podName,
					CacheSecretsSpcMapping: []*secretsstorev1.CacheSecretProviderClassSecrets{
						{
							SecretProviderClassName: spcName,
							CacheSecrets: []*secretsstorev1.CacheSecret{
								{
									SecretName:       "dummyS1",
									SecretType:       "Opaque",
									SecretObjectData: map[string][]byte{"dummyS1": {1, 2, 3}},
								},
							},
						},
					},
				},
			},
		},
		Status: secretsstorev1.SecretProviderCacheStatus{
			StatusFoo: "test status value",
		},
	}

	if err := c.Create(ctx, spCache); err == nil || !apierrors.IsAlreadyExists(err) {
		klog.Info("SecretProviderCache: failed to create secret provider cache!!")
		return err
	}

	klog.Info("SecretProviderCache: successfully created")
	spCacheUpdate := &secretsstorev1.SecretProviderCache{}
	klog.InfoS("SecretProviderCache: before getting", "spCacheUpdate", spCacheUpdate)
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
	var foundPod bool
	// check to see if we need to update the pod secrets or if we need to add a new pod
	for _, pod := range spCacheUpdate.Spec.PodSecrets {
		// the pod wasn't found yet, continue
		if pod.PodName != podName {
			continue
		}

		// the pod was found, we need to check if the secret provider class is already present
		foundPod = true
		var foundSpc bool
		for _, cachedPodSecrets := range pod.CacheSecretsSpcMapping {
			// the secret provider class wasn't found yet, continue
			if cachedPodSecrets.SecretProviderClassName != spcName {
				continue
			}

			// we found the spc, update the secrets in the cache
			foundSpc = true
			var unfoundSecrets map[string]string
			// update the secrets in the secret provider class
			for secretName, secretValue := range secrets {
				// if we found the secret, we need to update it	in the cache
				for _, cacheSecret := range cachedPodSecrets.CacheSecrets {
					if cacheSecret.SecretName == secretName {
						cacheSecret.SecretObjectData = map[string][]byte{secretName: []byte(secretValue)}
						if _, ok := unfoundSecrets[secretName]; ok {
							delete(unfoundSecrets, secretName)
						}
						break
					}
				}
			}

			// todo: move this to a function - addMissingSecretsToCache
			addMissingSecretsToCache(unfoundSecrets, &cachedPodSecrets.CacheSecrets)
		}

		// we didn't find the secretProvider class for the pod, we need to add it to the cache
		if !foundSpc {
			// add a new secret provider class
			addSecretProviderClassWithSecrets(&pod.CacheSecretsSpcMapping, spcName, secrets)
		}
	}

	// we couldn't find the pod, so we have to add it to the cache
	if !foundPod {
		// pod doesn't exist, add it
		spCacheUpdate.Spec.PodSecrets = append(spCacheUpdate.Spec.PodSecrets, &secretsstorev1.CachePodSecrets{
			PodName: podName,
		})
		addSecretProviderClassWithSecrets(&spCacheUpdate.Spec.PodSecrets[len(spCacheUpdate.Spec.PodSecrets)-1].CacheSecretsSpcMapping, spcName, secrets)
	}

	// update the labels of the secret provider class pod status to match the node label
	spCacheUpdate.Labels[secretsstorev1.InternalNodeLabel] = nodeID
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
