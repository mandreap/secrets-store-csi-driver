/*
Copyright 2018 The Kubernetes Authors.

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
	"crypto/rand"
	"os"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/secrets-store-csi-driver/pkg/k8s"
	"sigs.k8s.io/secrets-store-csi-driver/pkg/version"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	mount "k8s.io/mount-utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SecretsStore implements the IdentityServer, ControllerServer and
// NodeServer CSI interfaces.
type SecretsStore struct {
	endpoint string

	ns                 *nodeServer
	cs                 *controllerServer
	ids                *identityServer
	cacheEncryptionKey *corev1.Secret
}

func generateCacheEncryptionKey(ctx context.Context, client client.Client,
	reader client.Reader) (*corev1.Secret, error) {
	// check if the key is stored in the system
	cacheEncryptionKey := &corev1.Secret{}
	err := reader.Get(ctx, types.NamespacedName{Name: "secrets-store-csi-driver-cache-encryption-key", Namespace: "kube-system"}, cacheEncryptionKey)
	if err == nil {
		return cacheEncryptionKey, nil
	}
	//TODO: check what we should return here
	if !apierrors.IsNotFound(err) {
		klog.ErrorS(err, "failed to get cache encryption key from the system")
	} else {
		klog.InfoS("cache encryption key not found in the system")
	}

	// generate a new key with a finlaizer to avoid accidental deletion
	cacheEncryptionKey = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"secrets-store.csi.k8s.io/finalizer-cache-encryption-key"},
			Name:       "secrets-store-csi-driver-cache-encryption-key",
			Namespace:  "kube-system",
		},
		Type: corev1.SecretTypeOpaque,
	}
	// generate a random 32 byte key
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		klog.ErrorS(err, "failed to generate cache encryption key")
		return nil, err
	}
	cacheEncryptionKey.Data = map[string][]byte{
		"key": key,
	}
	err = client.Create(ctx, cacheEncryptionKey)
	if err != nil {
		klog.ErrorS(err, "failed to create cache encryption key")
		return nil, err
	}
	return cacheEncryptionKey, nil
}

// Add key creation here
// make sure to add finalizers for the key
func NewSecretsStoreDriver(driverName, nodeID, endpoint string,
	providerClients *PluginClientBuilder,
	client client.Client,
	reader client.Reader,
	tokenClient *k8s.TokenClient) *SecretsStore {
	klog.InfoS("Initializing Secrets Store CSI Driver", "driver", driverName, "version", version.BuildVersion, "buildTime", version.BuildTime)

	sr, err := NewStatsReporter()
	if err != nil {
		klog.ErrorS(err, "failed to initialize stats reporter")
		os.Exit(1)
	}
	ns, err := newNodeServer(nodeID, mount.New(""), providerClients, client, reader, sr, tokenClient)
	if err != nil {
		klog.ErrorS(err, "failed to initialize node server")
		os.Exit(1)
	}

	cacheEncryptionKey, err := generateCacheEncryptionKey(context.Background(), client, reader)
	if err != nil {
		klog.ErrorS(err, "failed to generate cache encryption key")
		os.Exit(1)
	}
	return &SecretsStore{
		endpoint:           endpoint,
		ns:                 ns,
		cs:                 newControllerServer(),
		ids:                newIdentityServer(driverName, version.BuildVersion),
		cacheEncryptionKey: cacheEncryptionKey,
	}
}

func newNodeServer(nodeID string,
	mounter mount.Interface,
	providerClients *PluginClientBuilder,
	client client.Client,
	reader client.Reader,
	statsReporter StatsReporter,
	tokenClient *k8s.TokenClient) (*nodeServer, error) {
	return &nodeServer{
		mounter:         mounter,
		reporter:        statsReporter,
		nodeID:          nodeID,
		client:          client,
		reader:          reader,
		providerClients: providerClients,
		tokenClient:     tokenClient,
	}, nil
}

// Run starts the CSI plugin
func (s *SecretsStore) Run(ctx context.Context) {
	server := NewNonBlockingGRPCServer()
	server.Start(ctx, s.endpoint, s.ids, s.cs, s.ns)
	server.Wait()
}
