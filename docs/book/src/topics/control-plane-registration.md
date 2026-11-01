# Control-plane backend registration

CAPG's default `Immediate` policy adds a running control-plane VM to its API
instance group. Load-balancer health checks then decide whether to serve traffic
to it. Existing clusters retain this behavior when the policy is omitted.

Internal passthrough load balancers need additional care during a control-plane
join: the GCE guest agent installs the load-balancer VIP as a local route on
registered backends. A joining machine can consequently send its discovery
requests to itself before its local API server exists. See Google's
[backend-as-client behavior](https://cloud.google.com/load-balancing/docs/internal/setting-up-internal#test_the_load_balancer).

For control planes that register their Kubernetes Node only after the local API
server starts, select `AfterNodeReference`:

```yaml
spec:
  loadBalancer:
    loadBalancerType: Internal
    controlPlaneRegistration: AfterNodeReference
```

After the cluster control plane initializes, CAPG waits for the corresponding
Machine's `status.nodeRef` before adding a new backend. The initial control plane
is still registered immediately so a new cluster can bootstrap. Infrastructure
readiness and ProviderID are published while registration is deferred, allowing
CAPI to discover the Node and reconcile its backend membership.

Kubeadm with `ControlPlaneKubeletLocalMode` enabled meets this ordering. KCP
defaults that feature on for supported Kubernetes versions where the feature
exists, unless the user explicitly disables it. Keep `Immediate` for control
planes that do not register Kubernetes Nodes or whose Node registration precedes
their local API server. NodeRef is not a universal control-plane health signal.

The policy is explicit and applies to the shared instance groups for every
configured load balancer, including `InternalExternal`. Changing it does not
remove existing members. Other load-balancer settings remain immutable.

Use the normal GCE guest environment to install the VIP route after registration.
CAPG does not require a guest routing script, a particular init system, or a
bootstrap-provider-specific file. Its health checks remain responsible for
backend health. This policy does not change the machine's IP-forwarding setting.
