//go:build !ignore_autogenerated
// +build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ByPodStatus) DeepCopyInto(out *ByPodStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ByPodStatus.
func (in *ByPodStatus) DeepCopy() *ByPodStatus {
	if in == nil {
		return nil
	}
	out := new(ByPodStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheFile) DeepCopyInto(out *CacheFile) {
	*out = *in
	if in.Contents != nil {
		in, out := &in.Contents, &out.Contents
		*out = make([]byte, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheFile.
func (in *CacheFile) DeepCopy() *CacheFile {
	if in == nil {
		return nil
	}
	out := new(CacheFile)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheSpcWorkloadFiles) DeepCopyInto(out *CacheSpcWorkloadFiles) {
	*out = *in
	if in.SecretFiles != nil {
		in, out := &in.SecretFiles, &out.SecretFiles
		*out = new([]*CacheFile)
		if **in != nil {
			in, out := *in, *out
			*out = make([]*CacheFile, len(*in))
			for i := range *in {
				if (*in)[i] != nil {
					in, out := &(*in)[i], &(*out)[i]
					*out = new(CacheFile)
					(*in).DeepCopyInto(*out)
				}
			}
		}
	}
	if in.WorkloadsMap != nil {
		in, out := &in.WorkloadsMap, &out.WorkloadsMap
		*out = make(map[string]*CacheWorkload, len(*in))
		for key, val := range *in {
			var outVal *CacheWorkload
			if val == nil {
				(*out)[key] = nil
			} else {
				in, out := &val, &outVal
				*out = new(CacheWorkload)
				(*in).DeepCopyInto(*out)
			}
			(*out)[key] = outVal
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheSpcWorkloadFiles.
func (in *CacheSpcWorkloadFiles) DeepCopy() *CacheSpcWorkloadFiles {
	if in == nil {
		return nil
	}
	out := new(CacheSpcWorkloadFiles)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CacheWorkload) DeepCopyInto(out *CacheWorkload) {
	*out = *in
	if in.CachedPods != nil {
		in, out := &in.CachedPods, &out.CachedPods
		*out = make(map[string]EmptyStruct, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CacheWorkload.
func (in *CacheWorkload) DeepCopy() *CacheWorkload {
	if in == nil {
		return nil
	}
	out := new(CacheWorkload)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EmptyStruct) DeepCopyInto(out *EmptyStruct) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EmptyStruct.
func (in *EmptyStruct) DeepCopy() *EmptyStruct {
	if in == nil {
		return nil
	}
	out := new(EmptyStruct)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretObject) DeepCopyInto(out *SecretObject) {
	*out = *in
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Annotations != nil {
		in, out := &in.Annotations, &out.Annotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Data != nil {
		in, out := &in.Data, &out.Data
		*out = make([]*SecretObjectData, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(SecretObjectData)
				**out = **in
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretObject.
func (in *SecretObject) DeepCopy() *SecretObject {
	if in == nil {
		return nil
	}
	out := new(SecretObject)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretObjectData) DeepCopyInto(out *SecretObjectData) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretObjectData.
func (in *SecretObjectData) DeepCopy() *SecretObjectData {
	if in == nil {
		return nil
	}
	out := new(SecretObjectData)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderCache) DeepCopyInto(out *SecretProviderCache) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderCache.
func (in *SecretProviderCache) DeepCopy() *SecretProviderCache {
	if in == nil {
		return nil
	}
	out := new(SecretProviderCache)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SecretProviderCache) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderCacheList) DeepCopyInto(out *SecretProviderCacheList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]SecretProviderCache, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderCacheList.
func (in *SecretProviderCacheList) DeepCopy() *SecretProviderCacheList {
	if in == nil {
		return nil
	}
	out := new(SecretProviderCacheList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SecretProviderCacheList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderCacheSpec) DeepCopyInto(out *SecretProviderCacheSpec) {
	*out = *in
	if in.SpcFilesWorkloads != nil {
		in, out := &in.SpcFilesWorkloads, &out.SpcFilesWorkloads
		*out = new(CacheSpcWorkloadFiles)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderCacheSpec.
func (in *SecretProviderCacheSpec) DeepCopy() *SecretProviderCacheSpec {
	if in == nil {
		return nil
	}
	out := new(SecretProviderCacheSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderCacheStatus) DeepCopyInto(out *SecretProviderCacheStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderCacheStatus.
func (in *SecretProviderCacheStatus) DeepCopy() *SecretProviderCacheStatus {
	if in == nil {
		return nil
	}
	out := new(SecretProviderCacheStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderClass) DeepCopyInto(out *SecretProviderClass) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderClass.
func (in *SecretProviderClass) DeepCopy() *SecretProviderClass {
	if in == nil {
		return nil
	}
	out := new(SecretProviderClass)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SecretProviderClass) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderClassList) DeepCopyInto(out *SecretProviderClassList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]SecretProviderClass, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderClassList.
func (in *SecretProviderClassList) DeepCopy() *SecretProviderClassList {
	if in == nil {
		return nil
	}
	out := new(SecretProviderClassList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SecretProviderClassList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderClassObject) DeepCopyInto(out *SecretProviderClassObject) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderClassObject.
func (in *SecretProviderClassObject) DeepCopy() *SecretProviderClassObject {
	if in == nil {
		return nil
	}
	out := new(SecretProviderClassObject)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderClassPodStatus) DeepCopyInto(out *SecretProviderClassPodStatus) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderClassPodStatus.
func (in *SecretProviderClassPodStatus) DeepCopy() *SecretProviderClassPodStatus {
	if in == nil {
		return nil
	}
	out := new(SecretProviderClassPodStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SecretProviderClassPodStatus) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderClassPodStatusList) DeepCopyInto(out *SecretProviderClassPodStatusList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]SecretProviderClassPodStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderClassPodStatusList.
func (in *SecretProviderClassPodStatusList) DeepCopy() *SecretProviderClassPodStatusList {
	if in == nil {
		return nil
	}
	out := new(SecretProviderClassPodStatusList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SecretProviderClassPodStatusList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderClassPodStatusStatus) DeepCopyInto(out *SecretProviderClassPodStatusStatus) {
	*out = *in
	if in.Objects != nil {
		in, out := &in.Objects, &out.Objects
		*out = make([]SecretProviderClassObject, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderClassPodStatusStatus.
func (in *SecretProviderClassPodStatusStatus) DeepCopy() *SecretProviderClassPodStatusStatus {
	if in == nil {
		return nil
	}
	out := new(SecretProviderClassPodStatusStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderClassSpec) DeepCopyInto(out *SecretProviderClassSpec) {
	*out = *in
	if in.Parameters != nil {
		in, out := &in.Parameters, &out.Parameters
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.SecretObjects != nil {
		in, out := &in.SecretObjects, &out.SecretObjects
		*out = make([]*SecretObject, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(SecretObject)
				(*in).DeepCopyInto(*out)
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderClassSpec.
func (in *SecretProviderClassSpec) DeepCopy() *SecretProviderClassSpec {
	if in == nil {
		return nil
	}
	out := new(SecretProviderClassSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretProviderClassStatus) DeepCopyInto(out *SecretProviderClassStatus) {
	*out = *in
	if in.ByPod != nil {
		in, out := &in.ByPod, &out.ByPod
		*out = make([]*ByPodStatus, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(ByPodStatus)
				**out = **in
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretProviderClassStatus.
func (in *SecretProviderClassStatus) DeepCopy() *SecretProviderClassStatus {
	if in == nil {
		return nil
	}
	out := new(SecretProviderClassStatus)
	in.DeepCopyInto(out)
	return out
}
