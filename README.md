# FortiKonnector

Facade API for FortiGate's SDN integration with Kubernetes.

The SDN connector gets sad if a `network-status` annotation contains an entry without the `ips` key.

This project is a simple facade API that patches any pods with a `network-status` annotation that does not contain an `ips` key.

As a bonus feature, if the pod is a Kubevirt VM, it will grab the IPs from the VMI object and put that in the `network-status` annotation.
