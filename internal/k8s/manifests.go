// Package k8s generates and applies the Kubernetes objects that make up a
// deployed application. Manifests are authored as typed API structs so the
// same values feed both YAML rendering (for inspection) and server-side apply
// (for the live cluster) from a single source of truth.
package k8s

import (
	"fmt"

	"github.com/Dhruva430/vibsl/internal/job"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

// Generate renders the full set of Kubernetes objects for a (defaulted) spec,
// in dependency order (namespace first, RBAC before workloads, etc.). Every
// object has TypeMeta set so it round-trips cleanly to YAML and through
// server-side apply.
func Generate(s job.Spec) []runtime.Object {
	l := labels(s.Name)
	return []runtime.Object{
		namespace(s, l),
		serviceAccount(s, l),
		configMap(s, l),
		secret(s, l),
		role(s, l),
		roleBinding(s, l),
		deployment(s, l),
		service(s, l),
		ingress(s, l),
		hpa(s, l),
		podDisruptionBudget(s, l),
		networkPolicy(s, l),
	}
}

// labels are the recommended app.kubernetes.io/* labels stamped on every object.
func labels(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       name,
		"app.kubernetes.io/managed-by": "vibsl",
	}
}

func objMeta(name, ns string, l map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: ns, Labels: l}
}

// --- individual resources -------------------------------------------------

func namespace(s job.Spec, l map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{Name: s.Namespace, Labels: l},
	}
}

func serviceAccount(s job.Spec, l map[string]string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta:                     metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
		ObjectMeta:                   objMeta(s.Name, s.Namespace, l),
		AutomountServiceAccountToken: ptr.To(false),
	}
}

func configMap(s job.Spec, l map[string]string) *corev1.ConfigMap {
	data := map[string]string{}
	for k, v := range s.Env {
		data[k] = v
	}
	// Always provide at least one key so the ConfigMap is non-empty and the
	// envFrom reference in the Deployment is meaningful.
	if len(data) == 0 {
		data["APP_NAME"] = s.Name
	}
	return &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: objMeta(s.Name+"-config", s.Namespace, l),
		Data:       data,
	}
}

// secret renders a placeholder Opaque Secret. Values come from spec.Secrets
// (intended to be wired to a real secret manager in production); when none are
// supplied a single placeholder key is emitted so the reference resolves.
func secret(s job.Spec, l map[string]string) *corev1.Secret {
	data := map[string]string{}
	for k, v := range s.Secrets {
		data[k] = v
	}
	if len(data) == 0 {
		data["PLACEHOLDER"] = "replace-me"
	}
	return &corev1.Secret{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: objMeta(s.Name+"-secret", s.Namespace, l),
		Type:       corev1.SecretTypeOpaque,
		StringData: data,
	}
}

func role(s job.Spec, l map[string]string) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "Role"},
		ObjectMeta: objMeta(s.Name, s.Namespace, l),
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"configmaps", "secrets"},
			Verbs:     []string{"get", "list", "watch"},
		}},
	}
}

func roleBinding(s job.Spec, l map[string]string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta:   metav1.TypeMeta{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "RoleBinding"},
		ObjectMeta: objMeta(s.Name, s.Namespace, l),
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     s.Name,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      s.Name,
			Namespace: s.Namespace,
		}},
	}
}

func deployment(s job.Spec, l map[string]string) *appsv1.Deployment {
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/",
				Port: intstr.FromInt32(s.ContainerPort),
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       10,
	}
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: objMeta(s.Name, s.Namespace, l),
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(s.Replicas),
			Selector: &metav1.LabelSelector{MatchLabels: l},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: l},
				Spec: corev1.PodSpec{
					ServiceAccountName: s.Name,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						RunAsUser:    ptr.To(s.RunAsUser),
						RunAsGroup:   ptr.To(s.RunAsUser),
						FSGroup:      ptr.To(s.RunAsUser),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{{
						Name:  s.Name,
						Image: s.Image.Ref(),
						Ports: []corev1.ContainerPort{{
							Name:          "http",
							ContainerPort: s.ContainerPort,
						}},
						EnvFrom: []corev1.EnvFromSource{
							{ConfigMapRef: &corev1.ConfigMapEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: s.Name + "-config"},
							}},
							{SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: s.Name + "-secret"},
							}},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(s.Resources.CPURequest),
								corev1.ResourceMemory: resource.MustParse(s.Resources.MemoryRequest),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(s.Resources.CPULimit),
								corev1.ResourceMemory: resource.MustParse(s.Resources.MemoryLimit),
							},
						},
						LivenessProbe:  probe,
						ReadinessProbe: probe,
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.To(false),
							ReadOnlyRootFilesystem:   ptr.To(true),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						},
					}},
				},
			},
		},
	}
}

func service(s job.Spec, l map[string]string) *corev1.Service {
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: objMeta(s.Name, s.Namespace, l),
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: l,
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       s.ServicePort,
				TargetPort: intstr.FromString("http"),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

func ingress(s job.Spec, l map[string]string) *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix
	return &networkingv1.Ingress{
		TypeMeta:   metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"},
		ObjectMeta: objMeta(s.Name, s.Namespace, l),
		Spec: networkingv1.IngressSpec{
			IngressClassName: ptr.To("nginx"),
			Rules: []networkingv1.IngressRule{{
				Host: s.Host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: s.Name,
									Port: networkingv1.ServiceBackendPort{Number: s.ServicePort},
								},
							},
						}},
					},
				},
			}},
		},
	}
}

// hpa is always generated; when autoscaling is disabled it is pinned to the
// requested replica count so it never fights the Deployment.
func hpa(s job.Spec, l map[string]string) *autoscalingv2.HorizontalPodAutoscaler {
	min, max, target := s.Replicas, s.Replicas, int32(80)
	if s.Autoscaling.Enabled {
		min, max, target = s.Autoscaling.MinReplicas, s.Autoscaling.MaxReplicas, s.Autoscaling.TargetCPUUtilPct
	}
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	return &autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta:   metav1.TypeMeta{APIVersion: "autoscaling/v2", Kind: "HorizontalPodAutoscaler"},
		ObjectMeta: objMeta(s.Name, s.Namespace, l),
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       s.Name,
			},
			MinReplicas: ptr.To(min),
			MaxReplicas: max,
			Metrics: []autoscalingv2.MetricSpec{{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: ptr.To(target),
					},
				},
			}},
		},
	}
}

func podDisruptionBudget(s job.Spec, l map[string]string) *policyv1.PodDisruptionBudget {
	minAvail := intstr.FromInt32(1)
	return &policyv1.PodDisruptionBudget{
		TypeMeta:   metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"},
		ObjectMeta: objMeta(s.Name, s.Namespace, l),
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvail,
			Selector:     &metav1.LabelSelector{MatchLabels: l},
		},
	}
}

// networkPolicy denies all traffic by default, then allows ingress to the app
// port (from any source so the ingress controller can reach it) and egress to
// DNS plus general egress for outbound calls.
func networkPolicy(s job.Spec, l map[string]string) *networkingv1.NetworkPolicy {
	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	dnsPort := intstr.FromInt32(53)
	appPort := intstr.FromInt32(s.ContainerPort)
	return &networkingv1.NetworkPolicy{
		TypeMeta:   metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: objMeta(s.Name, s.Namespace, l),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: l},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &appPort}},
			}},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: &udp, Port: &dnsPort},
					{Protocol: &tcp, Port: &dnsPort},
				}},
				{}, // allow all other egress
			},
		},
	}
}

// ObjectID returns a "Kind/namespace/name" identifier for logging.
func ObjectID(o runtime.Object) string {
	gvk := o.GetObjectKind().GroupVersionKind()
	if m, ok := o.(metav1.Object); ok {
		if ns := m.GetNamespace(); ns != "" {
			return fmt.Sprintf("%s/%s/%s", gvk.Kind, ns, m.GetName())
		}
		return fmt.Sprintf("%s/%s", gvk.Kind, m.GetName())
	}
	return gvk.Kind
}
