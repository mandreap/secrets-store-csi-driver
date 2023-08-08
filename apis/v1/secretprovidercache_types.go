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

type CacheFile struct {
	// The relative path of the file within the mount.
	// May not be an absolute path.
	// May not contain the path element '..'.
	// May not start with the string '..'.
	Path string `protobuf:"bytes,1,opt,name=path,proto3" json:"path,omitempty"`
	// The mode bits used to set permissions on this file.
	// Must be a decimal value between 0 and 511.
	Mode int32 `protobuf:"varint,2,opt,name=mode,proto3" json:"mode,omitempty"`
	// The file contents.
	Contents []byte `protobuf:"bytes,3,opt,name=contents,proto3" json:"contents,omitempty"`
}
type EmptyStruct struct{}
type CacheWorkload struct {
	WorkloadName       string                 `json:"workloadName,omitempty"`
	OwnerReferenceUID  string                 `json:"workloadUID,omitempty"`
	OwnerReferenceName string                 `json:"ownerReferenceName,omitempty"`
	OwnerReferenceKind string                 `json:"ownerReferenceKind,omitempty"`
	CachedPods         map[string]EmptyStruct `json:"cachedPods,omitempty"`
}

type CacheSpcWorkloadFiles struct {
	//SpcName      string                    `json:"spcName,omitempty"`
	SecretFiles *[]*CacheFile `json:"secretFiles,omitempty"`
	// Map WorkloadName to CacheWorkload
	WorkloadsMap map[string]*CacheWorkload `json:"workloadsMap,omitempty"`
}

// SecretProviderCacheSpec defines the desired state of SecretProviderCache
type SecretProviderCacheSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	ServiceAccountName   string `json:"serviceAccountName,omitempty"`
	NodePublishSecretRef string `json:"nodePublishSecretRef,omitempty"`

	SecretProviderClassName string                 `json:"SecretProviderClassName,omitempty"`
	SpcFilesWorkloads       *CacheSpcWorkloadFiles `json:"spcFilesWorkloads,omitempty"`
}

// SecretProviderCacheStatus defines the observed state of SecretProviderCache
type SecretProviderCacheStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	//StatusSecretProviderClass     string `json:"statusSecretProviderClass,omitempty"`
	WarningNoPersistencyOnRestart bool `json:"warningNoPersistencyOnRestart,omitempty"`
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
