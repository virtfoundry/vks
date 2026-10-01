/*
Copyright 2026 The VirtFoundry Authors.

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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	virtfoundryv1alpha1 "github.com/virtfoundry/vks/api/v1alpha1"
)

const testNS = "default"

var _ = Describe("VKSCluster Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-cluster"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: testNS,
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind VKSCluster")
			resource := &virtfoundryv1alpha1.VKSCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err != nil {
				resource = &virtfoundryv1alpha1.VKSCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: testNS,
					},
					Spec: virtfoundryv1alpha1.VKSClusterSpec{
						KubernetesVersion: "v1.36.5",
						ControlPlane: virtfoundryv1alpha1.VKSControlPlaneSpec{
							ServiceType: "NodePort",
							Address:     "10.0.30.250",
							Port:        30443,
						},
						Workers: virtfoundryv1alpha1.VKSWorkersSpec{
							Count:       1,
							TemplateRef: virtfoundryv1alpha1.LocalObjectRef{Name: "ubuntu-node-1-36-5"},
							OfferingRef: virtfoundryv1alpha1.LocalObjectRef{Name: "medium"},
							NetworkRef:  virtfoundryv1alpha1.LocalObjectRef{Name: "default"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &virtfoundryv1alpha1.VKSCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
			By("Cleanup the specific resource instance VKSCluster")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should add the finalizer", func() {
			By("Reconciling the created resource")
			controllerReconciler := &VKSClusterReconciler{
				Client:             k8sClient,
				Scheme:             k8sClient.Scheme(),
				DefaultNodeAddress: "10.0.30.250",
				DefaultNodePort:    30443,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			resource := &virtfoundryv1alpha1.VKSCluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			Expect(resource.Finalizers).To(ContainElement(finalizerName))
		})
	})
})
