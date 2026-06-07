package k8s

import (
	"bytes"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

// RenderYAML marshals the given objects into a single multi-document YAML
// stream (each object separated by "---"), matching what a user would write by
// hand or pipe into `kubectl apply -f -`.
func RenderYAML(objs []runtime.Object) ([]byte, error) {
	var buf bytes.Buffer
	for i, o := range objs {
		b, err := yaml.Marshal(o)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", ObjectID(o), err)
		}
		if i > 0 {
			buf.WriteString("---\n")
		}
		buf.Write(b)
	}
	return buf.Bytes(), nil
}
