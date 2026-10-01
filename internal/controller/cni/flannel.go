/*
Copyright 2026 The VirtFoundry Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

    15|Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cni

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

//go:embed kube-flannel.yml
var flannelManifest []byte

// ApplyFlannel applies the pinned Flannel manifest (v0.26.7) to the tenant cluster.
func ApplyFlannel(ctx context.Context, restCfg *rest.Config) error {
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(restCfg)
	if err != nil {
		return err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(flannelManifest), 4096)
	for {
		var obj unstructured.Unstructured
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("decode flannel manifest: %w", err)
		}
		if len(obj.Object) == 0 {
			continue
		}
		if err := applyObject(ctx, dyn, mapper, &obj); err != nil {
			return fmt.Errorf("apply %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}
	}
}

func applyObject(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	var resource dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = "default"
			obj.SetNamespace(ns)
		}
		resource = dyn.Resource(mapping.Resource).Namespace(ns)
	} else {
		resource = dyn.Resource(mapping.Resource)
	}

	existing, err := resource.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = resource.Create(ctx, obj, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	_, err = resource.Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		// Immutable fields / race: leave existing CNI alone.
		if apierrors.IsInvalid(err) || apierrors.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}
