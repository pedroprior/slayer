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
