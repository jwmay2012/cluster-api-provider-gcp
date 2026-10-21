# Advance notice for Spot preemption

Spot Machines may request a 120-second metadata notice before Compute Engine
begins operating-system shutdown:

```yaml
spec:
  template:
    spec:
      provisioningModel: Spot
      preemptionNoticeDurationSeconds: 120
```

This is a `GCPMachineTemplate` fragment. The same optional field exists in a
`GCPMachine` spec. Accepted values are 0 and 120, and the field requires Spot
provisioning. Omission retains Google's default of no advance notice. The field
is immutable; use a replacement template for an existing MachineDeployment.

During the notice, the metadata value `instance/preempted` becomes `TRUE` while
the guest is still running. A guest listener must watch that metadata change;
a shutdown script or logind shutdown listener alone begins too late to use the
advance notice. The normal best-effort shutdown period follows. Do not assume
150 seconds of guaranteed remaining execution or treat notice as confirmation
that the VM has already stopped.

The provider passes this setting when creating an instance. Upgrading the
controller does not modify the scheduling configuration of existing VMs. Install
the updated provider CRDs/controller before applying templates using this field.

See [Google's Spot preemption process](https://cloud.google.com/compute/docs/instances/spot#preemption_process)
and [handling preemption](https://cloud.google.com/compute/docs/instances/create-use-spot#handle_preemption).
