/*
Copyright 2019 The Kubernetes Authors.

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

package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/scope"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newMachine(clusterName, machineName string) *clusterv1.Machine {
	return &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: clusterName,
			},
			Name:      machineName,
			Namespace: "default",
		},
	}
}

func newMachineWithInfrastructureRef(clusterName, machineName string) *clusterv1.Machine {
	m := newMachine(clusterName, machineName)
	m.Spec.InfrastructureRef = clusterv1.ContractVersionedObjectReference{
		Kind:     "GCPMachine",
		Name:     "gcp" + machineName,
		APIGroup: infrav1.GroupVersion.Group,
	}

	return m
}

func newCluster(name string) *clusterv1.Cluster {
	return &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
	}
}

func TestGCPMachineReconciler_GCPClusterToGCPMachines(t *testing.T) {
	g := NewWithT(t)

	ctx := context.TODO()

	scheme := runtime.NewScheme()
	g.Expect(infrav1.AddToScheme(scheme)).To(Succeed())
	g.Expect(clusterv1.AddToScheme(scheme)).To(Succeed())

	clusterName := "my-cluster"
	initObjects := []runtime.Object{
		newCluster(clusterName),
		// Create two Machines with an infrastructure ref and one without.
		newMachineWithInfrastructureRef(clusterName, "my-machine-0"),
		newMachineWithInfrastructureRef(clusterName, "my-machine-1"),
		newMachine(clusterName, "my-machine-2"),
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(initObjects...).Build()

	reconciler := &GCPMachineReconciler{
		Client: client,
	}

	fn := reconciler.GCPClusterToGCPMachines(ctrl.SetupSignalHandler())
	rr := fn(ctx, &infrav1.GCPCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       clusterName,
					Kind:       "Cluster",
					APIVersion: clusterv1.GroupVersion.String(),
				},
			},
		},
	})
	g.Expect(rr).To(HaveLen(2))
}

func TestRunningControlPlaneRequeuesUntilNodeRegistration(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(infrav1.AddToScheme(scheme)).To(Succeed())
	g.Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
	cluster := newCluster("my-cluster")
	cluster.Status.Initialization.ControlPlaneInitialized = ptr.To(true)
	machine := newMachine("my-cluster", "my-machine")
	machine.Labels[clusterv1.MachineControlPlaneLabel] = ""
	machine.Spec.Version = "v1.33.4"
	machine.Spec.FailureDomain = "us-central1-c"
	machine.Spec.Bootstrap.DataSecretName = ptr.To("bootstrap")
	gcpCluster := &infrav1.GCPCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
		Spec: infrav1.GCPClusterSpec{
			Project: "my-proj", Region: "us-central1",
			LoadBalancer: infrav1.LoadBalancerSpec{LoadBalancerType: ptr.To(infrav1.Internal)},
		},
	}
	gcpMachine := &infrav1.GCPMachine{ObjectMeta: metav1.ObjectMeta{Name: "my-machine", Namespace: "default"}}
	bootstrap := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bootstrap", Namespace: "default"},
		Data:       map[string][]byte{"value": []byte("test-bootstrap")},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gcpMachine, bootstrap).Build()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/projects/my-proj/zones/us-central1-c/instances/my-machine" {
			t.Errorf("unexpected Google API request: %s %s", request.Method, request.URL.Path)
			http.NotFound(w, request)
			return
		}
		if err := json.NewEncoder(w).Encode(&compute.Instance{Name: "my-machine", Status: string(infrav1.InstanceStatusRunning)}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	computeService, err := compute.NewService(ctx, option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	g.Expect(err).NotTo(HaveOccurred())
	clusterScope, err := scope.NewClusterScope(ctx, scope.ClusterScopeParams{
		Client: client, Cluster: cluster, GCPCluster: gcpCluster,
		GCPServices: scope.GCPServices{Compute: computeService},
	})
	g.Expect(err).NotTo(HaveOccurred())
	machineScope, err := scope.NewMachineScope(scope.MachineScopeParams{
		Client: client, Machine: machine, GCPMachine: gcpMachine, ClusterGetter: clusterScope,
	})
	g.Expect(err).NotTo(HaveOccurred())
	reconciler := &GCPMachineReconciler{Client: client}
	result, err := reconciler.reconcile(ctx, machineScope)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gcpMachine.Status.Ready).To(BeTrue(), "CAPI must be able to discover the Node before backend registration")
	g.Expect(gcpMachine.Spec.ProviderID).NotTo(BeNil())
	g.Expect(result.RequeueAfter).To(Equal(5 * time.Second))
}
