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
	"fmt"

	corev1 "k8s.io/api/core/v1"

	virtfoundryv1alpha1 "github.com/virtfoundry/vks/api/v1alpha1"
)

const (
	serviceTypeLoadBalancer = "LoadBalancer"
	serviceTypeNodePort     = "NodePort"

	defaultLBPort         int32 = 443
	defaultNodePort       int32 = 30443
	konnectivityLBPort    int32 = 8132
	konnectivityNodePort  int32 = 30132
)

// resolveServiceType returns LoadBalancer when unset (product default).
func resolveServiceType(spec virtfoundryv1alpha1.VKSControlPlaneSpec) string {
	switch spec.ServiceType {
	case serviceTypeNodePort:
		return serviceTypeNodePort
	case serviceTypeLoadBalancer, "":
		return serviceTypeLoadBalancer
	default:
		return serviceTypeLoadBalancer
	}
}

// resolveAPIPort picks API port by service type when spec.port is zero.
func resolveAPIPort(spec virtfoundryv1alpha1.VKSControlPlaneSpec, serviceType string, defaultNP int32) (int32, error) {
	port := spec.Port
	if port == 0 {
		if serviceType == serviceTypeNodePort {
			if defaultNP != 0 {
				return defaultNP, nil
			}
			return defaultNodePort, nil
		}
		return defaultLBPort, nil
	}
	if serviceType == serviceTypeNodePort && (port < 30000 || port > 32767) {
		return 0, fmt.Errorf("NodePort must be in 30000–32767, got %d", port)
	}
	return port, nil
}

func konnectivityServerPort(serviceType string) int32 {
	if serviceType == serviceTypeNodePort {
		return konnectivityNodePort
	}
	return konnectivityLBPort
}

// loadBalancerVIP reads the first IP/hostname from a Service status.
func loadBalancerVIP(svc *corev1.Service) string {
	if svc == nil {
		return ""
	}
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		if ing.IP != "" {
			return ing.IP
		}
		if ing.Hostname != "" {
			return ing.Hostname
		}
	}
	return ""
}
