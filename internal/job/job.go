// Package job defines the deployment-job domain model: the user-supplied
// specification, the tracked status, and the lifecycle phases a job moves
// through as it is processed by the pipeline.
package job

import (
	"fmt"
	"strings"
	"time"
)

// Phase is a coarse lifecycle stage of a job.
type Phase string

const (
	PhasePending   Phase = "pending"   // accepted, waiting in the queue
	PhaseCloning   Phase = "cloning"   // git clone in progress
	PhaseBuilding  Phase = "building"  // docker build in progress
	PhasePushing   Phase = "pushing"   // pushing image to the registry
	PhaseDeploying Phase = "deploying" // rendering + applying manifests
	PhaseSucceeded Phase = "succeeded" // terminal: all steps completed
	PhaseFailed    Phase = "failed"    // terminal: a step errored
)

// IsTerminal reports whether the phase is a final state.
func (p Phase) IsTerminal() bool {
	return p == PhaseSucceeded || p == PhaseFailed
}

// ImageSpec describes where the built image should be tagged and pushed.
type ImageSpec struct {
	Registry string `json:"registry,omitempty"` // e.g. "localhost:5001"; falls back to DEFAULT_REGISTRY
	Name     string `json:"name,omitempty"`     // e.g. "demo-app"; defaults to the job name
	Tag      string `json:"tag,omitempty"`      // e.g. "v1"; defaults to a short job id
}

// Ref returns the fully-qualified image reference, e.g.
// "localhost:5001/demo-app:v1".
func (i ImageSpec) Ref() string {
	if i.Registry == "" {
		return fmt.Sprintf("%s:%s", i.Name, i.Tag)
	}
	return fmt.Sprintf("%s/%s:%s", strings.TrimSuffix(i.Registry, "/"), i.Name, i.Tag)
}

// Resources mirrors the subset of Kubernetes resource requests/limits we expose.
type Resources struct {
	CPURequest    string `json:"cpu_request,omitempty"`
	MemoryRequest string `json:"memory_request,omitempty"`
	CPULimit      string `json:"cpu_limit,omitempty"`
	MemoryLimit   string `json:"memory_limit,omitempty"`
}

// Autoscaling configures the generated HorizontalPodAutoscaler.
type Autoscaling struct {
	Enabled            bool  `json:"enabled,omitempty"`
	MinReplicas        int32 `json:"min_replicas,omitempty"`
	MaxReplicas        int32 `json:"max_replicas,omitempty"`
	TargetCPUUtilPct   int32 `json:"target_cpu_utilization,omitempty"`
}

// Spec is the user-supplied deployment request. It is intentionally flat and
// JSON-friendly; everything the pipeline and manifest generator need lives here.
type Spec struct {
	Name          string            `json:"name"`                     // logical app name, used for k8s object names
	RepoURL       string            `json:"repo_url"`                 // git remote to clone
	GitRef        string            `json:"git_ref,omitempty"`        // branch/tag/sha; defaults to the remote HEAD
	Dockerfile    string            `json:"dockerfile,omitempty"`     // path within the context; defaults to "Dockerfile"
	BuildContext  string            `json:"build_context,omitempty"`  // subdir of the repo; defaults to "."
	Image         ImageSpec         `json:"image,omitempty"`          // where to tag/push the built image
	Namespace     string            `json:"namespace,omitempty"`      // target namespace; defaults to the app name
	Replicas      int32             `json:"replicas,omitempty"`       // deployment replicas; defaults to 1
	ContainerPort int32             `json:"container_port,omitempty"` // port the app listens on; defaults to 8080
	ServicePort   int32             `json:"service_port,omitempty"`   // service port; defaults to 80
	Host          string            `json:"host,omitempty"`           // ingress host; defaults to "<name>.local"
	RunAsUser     int64             `json:"run_as_user,omitempty"`    // numeric UID the container runs as; defaults to 65532 (distroless nonroot)
	Env           map[string]string `json:"env,omitempty"`            // non-secret env -> ConfigMap
	Secrets       map[string]string `json:"secrets,omitempty"`        // secret env -> Secret (placeholder values ok)
	Resources     Resources         `json:"resources,omitempty"`
	Autoscaling   Autoscaling       `json:"autoscaling,omitempty"`
}

// Job is the server-side record of a Spec plus its evolving status. It is the
// unit stored, returned by the API, and processed by the pipeline.
type Job struct {
	ID         string     `json:"id"`
	Spec       Spec       `json:"spec"`
	Phase      Phase      `json:"phase"`
	Message    string     `json:"message,omitempty"`     // human-readable detail for the current/last phase
	ImageRef   string     `json:"image_ref,omitempty"`   // resolved image reference once known
	Resources  []AppliedResource `json:"applied_resources,omitempty"` // k8s objects applied
	Logs       []LogEntry `json:"logs,omitempty"`        // ordered, structured pipeline log
	Error      string     `json:"error,omitempty"`       // populated on failure
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// AppliedResource identifies a Kubernetes object that the pipeline applied.
type AppliedResource struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// LogEntry is a single timestamped line in a job's pipeline log.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Phase   Phase     `json:"phase"`
	Message string    `json:"message"`
}
