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

package v1beta1

import (
	"context"
	"testing"

	"k8s.io/utils/ptr"
)

func TestPreemptionNoticeAdmission(t *testing.T) {
	for _, tt := range []struct {
		name    string
		spec    GCPMachineSpec
		invalid bool
	}{
		{name: "standard unchanged"},
		{name: "spot default", spec: GCPMachineSpec{ProvisioningModel: ptr.To(ProvisioningModelSpot)}},
		{name: "spot advance notice", spec: GCPMachineSpec{ProvisioningModel: ptr.To(ProvisioningModelSpot), PreemptionNoticeDurationSeconds: ptr.To[int64](120)}},
		{name: "explicit zero", spec: GCPMachineSpec{ProvisioningModel: ptr.To(ProvisioningModelSpot), PreemptionNoticeDurationSeconds: ptr.To[int64](0)}},
		{name: "unsupported duration", spec: GCPMachineSpec{ProvisioningModel: ptr.To(ProvisioningModelSpot), PreemptionNoticeDurationSeconds: ptr.To[int64](60)}, invalid: true},
		{name: "negative duration", spec: GCPMachineSpec{ProvisioningModel: ptr.To(ProvisioningModelSpot), PreemptionNoticeDurationSeconds: ptr.To[int64](-1)}, invalid: true},
		{name: "standard", spec: GCPMachineSpec{ProvisioningModel: ptr.To(ProvisioningModelStandard), PreemptionNoticeDurationSeconds: ptr.To[int64](120)}, invalid: true},
		{name: "missing provisioning model", spec: GCPMachineSpec{PreemptionNoticeDurationSeconds: ptr.To[int64](120)}, invalid: true},
		{name: "legacy preemptible", spec: GCPMachineSpec{Preemptible: true, PreemptionNoticeDurationSeconds: ptr.To[int64](120)}, invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			machine := &GCPMachine{Spec: tt.spec}
			_, machineErr := (&gcpMachineWebhook{}).ValidateCreate(context.Background(), machine)
			template := &GCPMachineTemplate{Spec: GCPMachineTemplateSpec{Template: GCPMachineTemplateResource{Spec: tt.spec}}}
			_, templateErr := (&gcpMachineTemplateWebhook{}).ValidateCreate(context.Background(), template)
			if (machineErr != nil) != tt.invalid || (templateErr != nil) != tt.invalid {
				t.Fatalf("invalid=%t, Machine error=%v, template error=%v", tt.invalid, machineErr, templateErr)
			}
		})
	}
}

func TestPreemptionNoticeIsImmutable(t *testing.T) {
	old := &GCPMachine{Spec: GCPMachineSpec{ProvisioningModel: ptr.To(ProvisioningModelSpot)}}
	updated := old.DeepCopy()
	updated.Spec.PreemptionNoticeDurationSeconds = ptr.To[int64](120)
	if _, err := (&gcpMachineWebhook{}).ValidateUpdate(context.Background(), old, updated); err == nil {
		t.Fatal("changing an existing Machine must require a replacement")
	}
	oldTemplate := &GCPMachineTemplate{Spec: GCPMachineTemplateSpec{Template: GCPMachineTemplateResource{Spec: old.Spec}}}
	newTemplate := &GCPMachineTemplate{Spec: GCPMachineTemplateSpec{Template: GCPMachineTemplateResource{Spec: updated.Spec}}}
	if _, err := (&gcpMachineTemplateWebhook{}).ValidateUpdate(context.Background(), oldTemplate, newTemplate); err == nil {
		t.Fatal("changing an existing template must require a new template")
	}
}
