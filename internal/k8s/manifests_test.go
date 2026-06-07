package k8s

import (
	"strings"
	"testing"

	"github.com/Dhruva430/vibsl/internal/job"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func testSpec() job.Spec {
	s := job.Spec{
		Name:    "demo-app",
		RepoURL: "https://example.com/repo.git",
		Env:     map[string]string{"GREETING": "hello"},
		Secrets: map[string]string{"API_KEY": "shh"},
		Autoscaling: job.Autoscaling{
			Enabled:     true,
			MinReplicas: 2,
			MaxReplicas: 5,
		},
	}
	return s.WithDefaults("localhost:5001", "v1")
}

// TestGenerateProducesAllRequiredKinds asserts that every required resource is
// emitted exactly once.
func TestGenerateProducesAllRequiredKinds(t *testing.T) {
	want := []string{
		"Namespace", "ServiceAccount", "ConfigMap", "Secret",
		"Role", "RoleBinding", "Deployment", "Service",
		"Ingress", "HorizontalPodAutoscaler", "PodDisruptionBudget", "NetworkPolicy",
	}
	objs := Generate(testSpec())
	got := map[string]int{}
	for _, o := range objs {
		got[o.GetObjectKind().GroupVersionKind().Kind]++
	}
	if len(objs) != len(want) {
		t.Fatalf("expected %d objects, got %d", len(want), len(objs))
	}
	for _, k := range want {
		if got[k] != 1 {
			t.Errorf("kind %s: expected exactly 1, got %d", k, got[k])
		}
	}
}

// TestNamespaceFirst ensures the namespace is applied before namespaced objects.
func TestNamespaceFirst(t *testing.T) {
	objs := Generate(testSpec())
	if objs[0].GetObjectKind().GroupVersionKind().Kind != "Namespace" {
		t.Fatalf("first object must be the Namespace, got %s",
			objs[0].GetObjectKind().GroupVersionKind().Kind)
	}
}

// TestDeploymentWiring checks the image, replicas, SA, and env wiring.
func TestDeploymentWiring(t *testing.T) {
	dep := findKind[*appsv1.Deployment](t, Generate(testSpec()))
	if got := dep.Spec.Template.Spec.Containers[0].Image; got != "localhost:5001/demo-app:v1" {
		t.Errorf("image = %q, want localhost:5001/demo-app:v1", got)
	}
	if dep.Spec.Template.Spec.ServiceAccountName != "demo-app" {
		t.Errorf("service account = %q, want demo-app", dep.Spec.Template.Spec.ServiceAccountName)
	}
	c := dep.Spec.Template.Spec.Containers[0]
	if c.SecurityContext == nil || c.SecurityContext.ReadOnlyRootFilesystem == nil || !*c.SecurityContext.ReadOnlyRootFilesystem {
		t.Errorf("expected read-only root filesystem hardening")
	}
	if len(c.EnvFrom) != 2 {
		t.Errorf("expected envFrom configmap+secret, got %d sources", len(c.EnvFrom))
	}
}

// TestHPARespectsAutoscaling checks min/max come from the autoscaling block.
func TestHPARespectsAutoscaling(t *testing.T) {
	hpa := findKind[*autoscalingv2.HorizontalPodAutoscaler](t, Generate(testSpec()))
	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != 2 {
		t.Errorf("min replicas = %v, want 2", hpa.Spec.MinReplicas)
	}
	if hpa.Spec.MaxReplicas != 5 {
		t.Errorf("max replicas = %d, want 5", hpa.Spec.MaxReplicas)
	}
	if hpa.Spec.ScaleTargetRef.Name != "demo-app" {
		t.Errorf("scale target = %q, want demo-app", hpa.Spec.ScaleTargetRef.Name)
	}
}

// TestSecretPlaceholder verifies a placeholder key is emitted when none given.
func TestSecretPlaceholder(t *testing.T) {
	s := job.Spec{Name: "x", RepoURL: "r"}.WithDefaults("reg", "v1")
	sec := findKind[*corev1.Secret](t, Generate(s))
	if _, ok := sec.StringData["PLACEHOLDER"]; !ok {
		t.Errorf("expected PLACEHOLDER key when no secrets supplied")
	}
}

// TestRenderYAMLRoundTrips ensures every object renders with apiVersion+kind.
func TestRenderYAMLRoundTrips(t *testing.T) {
	out, err := RenderYAML(Generate(testSpec()))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, kind := range []string{"kind: Namespace", "kind: Deployment", "kind: NetworkPolicy"} {
		if !strings.Contains(s, kind) {
			t.Errorf("rendered YAML missing %q", kind)
		}
	}
	if strings.Count(s, "---") != 11 { // 12 docs => 11 separators
		t.Errorf("expected 11 document separators, got %d", strings.Count(s, "---"))
	}
}

// findKind locates the single object of type T in objs or fails the test.
func findKind[T runtime.Object](t *testing.T, objs []runtime.Object) T {
	t.Helper()
	for _, o := range objs {
		if v, ok := o.(T); ok {
			return v
		}
	}
	var zero T
	t.Fatalf("object of type %T not found", zero)
	return zero
}
