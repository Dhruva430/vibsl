package job

import (
	"fmt"
	"regexp"
	"strings"
)

// dns1123 is the label format Kubernetes requires for most object names.
var dns1123 = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Validate checks the user-supplied fields that we cannot safely default.
func (s Spec) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !dns1123.MatchString(s.Name) {
		return fmt.Errorf("name %q must be a DNS-1123 label (lowercase alphanumeric and '-')", s.Name)
	}
	if strings.TrimSpace(s.RepoURL) == "" {
		return fmt.Errorf("repo_url is required")
	}
	if s.Namespace != "" && !dns1123.MatchString(s.Namespace) {
		return fmt.Errorf("namespace %q must be a DNS-1123 label", s.Namespace)
	}
	if s.Replicas < 0 {
		return fmt.Errorf("replicas must be >= 0")
	}
	if s.Autoscaling.Enabled {
		if s.Autoscaling.MinReplicas < 1 {
			return fmt.Errorf("autoscaling.min_replicas must be >= 1 when enabled")
		}
		if s.Autoscaling.MaxReplicas < s.Autoscaling.MinReplicas {
			return fmt.Errorf("autoscaling.max_replicas must be >= min_replicas")
		}
	}
	return nil
}

// WithDefaults returns a copy of the spec with all optional fields filled in.
// shortID seeds the default image tag so repeated submissions stay distinct.
func (s Spec) WithDefaults(defaultRegistry, shortID string) Spec {
	out := s
	if out.Namespace == "" {
		out.Namespace = out.Name
	}
	if out.Dockerfile == "" {
		out.Dockerfile = "Dockerfile"
	}
	if out.BuildContext == "" {
		out.BuildContext = "."
	}
	if out.GitRef == "" {
		out.GitRef = "" // empty => clone the remote's default HEAD
	}
	if out.Replicas == 0 {
		out.Replicas = 1
	}
	if out.ContainerPort == 0 {
		out.ContainerPort = 8080
	}
	if out.ServicePort == 0 {
		out.ServicePort = 80
	}
	if out.Host == "" {
		out.Host = out.Name + ".local"
	}
	if out.RunAsUser == 0 {
		// 65532 is the conventional "nonroot" UID used by distroless and many
		// hardened base images; a numeric UID lets the kubelet verify
		// runAsNonRoot, which it cannot do for a named user.
		out.RunAsUser = 65532
	}
	if out.Image.Registry == "" {
		out.Image.Registry = defaultRegistry
	}
	if out.Image.Name == "" {
		out.Image.Name = out.Name
	}
	if out.Image.Tag == "" {
		out.Image.Tag = shortID
	}
	if out.Resources.CPURequest == "" {
		out.Resources.CPURequest = "50m"
	}
	if out.Resources.MemoryRequest == "" {
		out.Resources.MemoryRequest = "64Mi"
	}
	if out.Resources.CPULimit == "" {
		out.Resources.CPULimit = "250m"
	}
	if out.Resources.MemoryLimit == "" {
		out.Resources.MemoryLimit = "128Mi"
	}
	if out.Autoscaling.Enabled {
		if out.Autoscaling.TargetCPUUtilPct == 0 {
			out.Autoscaling.TargetCPUUtilPct = 80
		}
	}
	return out
}
