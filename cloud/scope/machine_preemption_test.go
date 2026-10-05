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

package scope

import (
	"encoding/json"
	"testing"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/api/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

func TestInstancePreemptionNotice(t *testing.T) {
	for _, tt := range []struct {
		name    string
		seconds *int64
		want    string
	}{
		{name: "omitted remains absent"},
		{name: "advance notice", seconds: ptr.To[int64](120), want: "120"},
		{name: "explicit zero is sent", seconds: ptr.To[int64](0), want: "0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			scope := &MachineScope{
				Machine: &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: ptr.To("us-central1-c")}},
				GCPMachine: &infrav1.GCPMachine{ObjectMeta: metav1.ObjectMeta{Name: "spot-test"}, Spec: infrav1.GCPMachineSpec{
					InstanceType: "n2-standard-8", ProvisioningModel: ptr.To(infrav1.ProvisioningModelSpot), PreemptionNoticeDurationSeconds: tt.seconds,
				}},
				ClusterGetter: &ClusterScope{Cluster: &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test"}}, GCPCluster: &infrav1.GCPCluster{}},
			}
			encoded, err := json.Marshal(scope.InstanceSpec(logr.Discard()))
			if err != nil {
				t.Fatal(err)
			}
			var request struct {
				Scheduling struct {
					ProvisioningModel string `json:"provisioningModel"`
					Notice            *struct {
						Seconds string `json:"seconds"`
					} `json:"preemptionNoticeDuration"`
				} `json:"scheduling"`
			}
			if err := json.Unmarshal(encoded, &request); err != nil {
				t.Fatal(err)
			}
			if request.Scheduling.ProvisioningModel != "SPOT" {
				t.Fatalf("unexpected provisioning model: %s", encoded)
			}
			if tt.seconds == nil {
				if request.Scheduling.Notice != nil {
					t.Fatalf("default changed: %s", encoded)
				}
			} else if request.Scheduling.Notice == nil || request.Scheduling.Notice.Seconds != tt.want {
				t.Fatalf("notice was not sent to Compute Engine: %s", encoded)
			}
		})
	}
}
