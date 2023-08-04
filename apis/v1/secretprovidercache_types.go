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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type CacheSecret struct {
	// SecretName       string            `json:"secretName,omitempty"`
	SecretType       string            `json:"secretType,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Annotations      map[string]string `json:"annotations,omitempty"`
	SecretObjectData map[string][]byte `json:"secretObjectData,omitempty"`
}

type CacheSecretProviderClassSecrets struct {
	// SecretProviderClassName string `json:"secretProviderClassName,omitempty"`
	// map of secretName to secret
	CacheSecretsMap map[string]*CacheSecret `json:"cacheSecretsMap,omitempty"`
}

type CachePodSecrets struct {
	// PodName string `json:"podName,omitempty"`
	// map of secretProviderClassName to secrets
	CacheSecretsSpcMapping map[string]*CacheSecretProviderClassSecrets `json:"cacheSecretsSpcMapping,omitempty"`
	// each pod can have a service principal associated with it
	ServicePrincipalNodeRef string `json:"servicePrincipalNodeRef,omitempty"`
}

// SecretProviderCacheSpec defines the desired state of SecretProviderCache
type SecretProviderCacheSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// identifier should be unique
	// we can have either have a SP with password or federated workload identities
	// map of podName to podSecrets
	PodSecretsMap map[string]*CachePodSecrets `json:"podSecretsMap,omitempty"`
}

// SecretProviderCacheStatus defines the observed state of SecretProviderCache
type SecretProviderCacheStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	StatusSecretProviderClass string `json:"statusSecretProviderClass,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +genclient
// +kubebuilder:subresource:status
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SecretProviderCache is the Schema for the secretprovidercaches API
type SecretProviderCache struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SecretProviderCacheSpec   `json:"spec,omitempty"`
	Status SecretProviderCacheStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SecretProviderCacheList contains a list of SecretProviderCache
type SecretProviderCacheList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecretProviderCache `json:"items"`
}
