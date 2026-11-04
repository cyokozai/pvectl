package lxc

// minimalSpec is the smallest spec validateSpec accepts.
func minimalSpec() *Spec {
	return &Spec{
		TargetNode:   "pve1",
		Unprivileged: boolPtr(true),
	}
}
