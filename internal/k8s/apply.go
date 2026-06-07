package k8s

import (
	"context"
	"fmt"
	"io"

	"github.com/Dhruva430/vibsl/internal/job"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// Applier applies arbitrary typed objects to a cluster using server-side apply
// via the dynamic client. A discovery-backed RESTMapper resolves each object's
// GroupVersionKind to the REST resource, so no per-type wiring is needed and
// new resource kinds work without code changes.
type Applier struct {
	dyn          dynamic.Interface
	mapper       meta.RESTMapper
	fieldManager string
}

// NewApplier builds an Applier from a kubeconfig path (empty => default
// loading rules: KUBECONFIG env, then ~/.kube/config, then in-cluster).
func NewApplier(kubeconfig, fieldManager string) (*Applier, error) {
	cfg, err := restConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))
	return &Applier{dyn: dyn, mapper: mapper, fieldManager: fieldManager}, nil
}

func restConfig(kubeconfig string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return cfg, nil
}

// Apply server-side-applies a single object and returns the identifier of the
// resource that was created or updated.
func (a *Applier) Apply(ctx context.Context, obj runtime.Object) (job.AppliedResource, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return job.AppliedResource{}, err
	}
	gvk := u.GroupVersionKind()
	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return job.AppliedResource{}, fmt.Errorf("rest mapping for %s: %w", gvk, err)
	}

	var ri dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ri = a.dyn.Resource(mapping.Resource).Namespace(u.GetNamespace())
	} else {
		ri = a.dyn.Resource(mapping.Resource)
	}

	data, err := u.MarshalJSON()
	if err != nil {
		return job.AppliedResource{}, fmt.Errorf("marshal %s: %w", ObjectID(obj), err)
	}

	applied, err := ri.Patch(ctx, u.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: a.fieldManager,
		Force:        ptrBool(true),
	})
	if err != nil {
		return job.AppliedResource{}, fmt.Errorf("apply %s: %w", ObjectID(obj), err)
	}
	return job.AppliedResource{
		Kind:      applied.GetKind(),
		Name:      applied.GetName(),
		Namespace: applied.GetNamespace(),
	}, nil
}

// ApplyAll applies objects in order, logging each result. It stops at the first
// error so callers see a precise failure point.
func (a *Applier) ApplyAll(ctx context.Context, objs []runtime.Object, logw io.Writer) ([]job.AppliedResource, error) {
	out := make([]job.AppliedResource, 0, len(objs))
	for _, o := range objs {
		res, err := a.Apply(ctx, o)
		if err != nil {
			fmt.Fprintf(logw, "apply %s: ERROR %v\n", ObjectID(o), err)
			return out, err
		}
		fmt.Fprintf(logw, "applied %s\n", ObjectID(o))
		out = append(out, res)
	}
	return out, nil
}

func toUnstructured(obj runtime.Object) (*unstructured.Unstructured, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("to unstructured: %w", err)
	}
	return &unstructured.Unstructured{Object: m}, nil
}

func ptrBool(b bool) *bool { return &b }
