/*
Copyright 2026 The Kubernetes Authors.

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

package instances

import (
	"context"
	"testing"

	k8scloud "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud/scope"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type recordingInstanceGroups struct {
	members     []*compute.InstanceWithNamedPorts
	addCalls    int
	removeCalls int
}

func (g *recordingInstanceGroups) ListInstances(context.Context, *meta.Key, *compute.InstanceGroupsListInstancesRequest, *filter.F, ...k8scloud.Option) ([]*compute.InstanceWithNamedPorts, error) {
	return g.members, nil
}

func (g *recordingInstanceGroups) AddInstances(_ context.Context, _ *meta.Key, request *compute.InstanceGroupsAddInstancesRequest, _ ...k8scloud.Option) error {
	g.addCalls++
	for _, instance := range request.Instances {
		g.members = append(g.members, &compute.InstanceWithNamedPorts{Instance: instance.Instance})
	}
	return nil
}

func (g *recordingInstanceGroups) RemoveInstances(context.Context, *meta.Key, *compute.InstanceGroupsRemoveInstancesRequest, ...k8scloud.Option) error {
	g.removeCalls++
	return nil
}

func TestControlPlaneRegistration(t *testing.T) {
	for _, test := range []struct {
		name          string
		lbType        infrav1.LoadBalancerType
		policy        *infrav1.ControlPlaneRegistrationPolicy
		initialized   *bool
		nodeName      string
		worker        bool
		registered    bool
		registerLater bool
		wantAdds      int
	}{
		{name: "default preserves control planes without Nodes", lbType: infrav1.Internal, initialized: ptr.To(true), wantAdds: 1},
		{name: "explicit Immediate preserves registration", lbType: infrav1.Internal, policy: ptr.To(infrav1.ControlPlaneRegistrationImmediate), initialized: ptr.To(true), wantAdds: 1},
		{name: "first control plane before initialization status", lbType: infrav1.Internal, policy: ptr.To(infrav1.ControlPlaneRegistrationAfterNodeReference), wantAdds: 1},
		{name: "first control plane with initialization false", lbType: infrav1.Internal, policy: ptr.To(infrav1.ControlPlaneRegistrationAfterNodeReference), initialized: ptr.To(false), wantAdds: 1},
		{name: "replacement waits for Node then registers", lbType: infrav1.Internal, policy: ptr.To(infrav1.ControlPlaneRegistrationAfterNodeReference), initialized: ptr.To(true), registerLater: true},
		{name: "joined replacement registers", lbType: infrav1.Internal, policy: ptr.To(infrav1.ControlPlaneRegistrationAfterNodeReference), initialized: ptr.To(true), nodeName: "my-node", wantAdds: 1},
		{name: "combined load balancer waits for Node", lbType: infrav1.InternalExternal, policy: ptr.To(infrav1.ControlPlaneRegistrationAfterNodeReference), initialized: ptr.To(true), registerLater: true},
		{name: "external load balancer preserves registration", lbType: infrav1.External, initialized: ptr.To(true), wantAdds: 1},
		{name: "external load balancer can explicitly defer registration", lbType: infrav1.External, policy: ptr.To(infrav1.ControlPlaneRegistrationAfterNodeReference), initialized: ptr.To(true), registerLater: true},
		{name: "existing backend is preserved", lbType: infrav1.Internal, policy: ptr.To(infrav1.ControlPlaneRegistrationAfterNodeReference), initialized: ptr.To(true), registered: true},
		{name: "worker is never registered", lbType: infrav1.Internal, policy: ptr.To(infrav1.ControlPlaneRegistrationAfterNodeReference), initialized: ptr.To(true), worker: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(fakeBootstrapSecret.DeepCopy()).Build()
			cluster := fakeCluster.DeepCopy()
			cluster.Status.Initialization.ControlPlaneInitialized = test.initialized
			gcpCluster := fakeGCPCluster.DeepCopy()
			gcpCluster.Spec.LoadBalancer.LoadBalancerType = ptr.To(test.lbType)
			gcpCluster.Spec.LoadBalancer.ControlPlaneRegistration = test.policy
			clusterScope, err := scope.NewClusterScope(ctx, scope.ClusterScopeParams{
				Client: client, Cluster: cluster, GCPCluster: gcpCluster,
				GCPServices: scope.GCPServices{Compute: &compute.Service{}},
			})
			if err != nil {
				t.Fatal(err)
			}
			machine := fakeMachine.DeepCopy()
			machine.Status.NodeRef.Name = test.nodeName
			if !test.worker {
				machine.Labels = map[string]string{clusterv1.MachineControlPlaneLabel: ""}
			}
			machineScope, err := scope.NewMachineScope(scope.MachineScopeParams{
				Client: client, Machine: machine, GCPMachine: getFakeGCPMachine(), ClusterGetter: clusterScope,
			})
			if err != nil {
				t.Fatal(err)
			}
			instance := &compute.Instance{Name: "my-machine", SelfLink: "projects/my-proj/zones/us-central1-c/instances/my-machine", Status: string(infrav1.InstanceStatusRunning)}
			groups := &recordingInstanceGroups{}
			if test.registered {
				groups.members = []*compute.InstanceWithNamedPorts{{Instance: instance.SelfLink}}
			}
			service := New(machineScope)
			service.instances = &k8scloud.MockInstances{
				ProjectRouter: &k8scloud.SingleProjectRouter{ID: "my-proj"},
				Objects: map[meta.Key]*k8scloud.MockInstancesObj{
					{Name: "my-machine", Zone: "us-central1-c"}: {Obj: instance},
				},
			}
			service.instancegroups = groups
			if err := service.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if groups.addCalls != test.wantAdds || groups.removeCalls != 0 {
				t.Fatalf("registration adds=%d removes=%d, want adds=%d removes=0", groups.addCalls, groups.removeCalls, test.wantAdds)
			}
			if machineScope.GetProviderID() == "" || *machineScope.GetInstanceStatus() != infrav1.InstanceStatusRunning {
				t.Fatal("deferring registration must still publish the running instance so CAPI can discover its Node")
			}
			if test.registerLater {
				machine.Status.NodeRef.Name = "my-node"
				for range 2 {
					if err := service.Reconcile(ctx); err != nil {
						t.Fatal(err)
					}
				}
				if groups.addCalls != 1 || len(groups.members) != 1 {
					t.Fatal("Node registration must add the backend exactly once on subsequent reconciliation")
				}
			}
			if test.registered && len(groups.members) != 1 {
				t.Fatal("existing backend membership changed")
			}
		})
	}
}
