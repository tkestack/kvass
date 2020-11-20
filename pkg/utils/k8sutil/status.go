package k8sutil

import v1 "k8s.io/api/core/v1"

// IsPodReady return true if Pod conditions is ready
func IsPodReady(p *v1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == v1.PodReady && c.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsNodeReady return true if Node conditions is ready
func IsNodeReady(n *v1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == v1.NodeReady && c.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}
