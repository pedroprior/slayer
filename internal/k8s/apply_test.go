package k8s

import "testing"

func TestSplitYAMLDocuments(t *testing.T) {
	input := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: a
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: b
`)

	docs, err := splitYAMLDocuments(input)
	if err != nil {
		t.Fatalf("splitYAMLDocuments() error = %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("splitYAMLDocuments() returned %d docs, want 2", len(docs))
	}
}

func TestSplitYAMLDocuments_SkipsEmptyDocuments(t *testing.T) {
	input := []byte("---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\n---\n")

	docs, err := splitYAMLDocuments(input)
	if err != nil {
		t.Fatalf("splitYAMLDocuments() error = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("splitYAMLDocuments() returned %d docs, want 1 (empty docs skipped)", len(docs))
	}
}

// Regression test: `helm template --include-crds` (used to vendor
// manifests/traefik.yaml) emits a "# Source: ..." comment as its own
// document ahead of some resources. Such a document is non-empty after
// TrimSpace but decodes to YAML null, which previously reached the k8s
// decoder and failed with "Object 'Kind' is missing in 'null'".
func TestSplitYAMLDocuments_SkipsCommentOnlyDocuments(t *testing.T) {
	input := []byte("# Source: chart/templates/a.yaml\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n")

	docs, err := splitYAMLDocuments(input)
	if err != nil {
		t.Fatalf("splitYAMLDocuments() error = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("splitYAMLDocuments() returned %d docs, want 1 (comment-only docs skipped)", len(docs))
	}
}
