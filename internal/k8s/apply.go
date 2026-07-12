package k8s

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// splitYAMLDocuments splits a multi-document YAML file on lines that are
// exactly "---" (the YAML document separator), skipping any resulting empty
// documents (e.g. a leading "---" or trailing "---" with nothing after it).
func splitYAMLDocuments(data []byte) ([][]byte, error) {
	var docs [][]byte
	var current bytes.Buffer

	flush := func() {
		if trimmed := bytes.TrimSpace(current.Bytes()); len(trimmed) > 0 {
			docs = append(docs, append([]byte{}, trimmed...))
		}
		current.Reset()
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			flush()
			continue
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning yaml documents: %w", err)
	}
	flush()

	return docs, nil
}

// ApplyManifest server-side-applies every document in the file at
// manifestPath against the cluster referenced by kubeconfigPath, using field
// manager "clusterctl" so re-running is idempotent.
func ApplyManifest(ctx context.Context, kubeconfigPath, manifestPath string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest %s: %w", manifestPath, err)
	}

	if err := ApplyManifestBytes(ctx, kubeconfigPath, data); err != nil {
		return fmt.Errorf("applying %s: %w", manifestPath, err)
	}
	return nil
}

// ApplyManifestBytes server-side-applies every document in a multi-document
// YAML blob (e.g. one rendered from a template rather than read verbatim
// from disk) against the cluster referenced by kubeconfigPath, using field
// manager "clusterctl" so re-running is idempotent.
func ApplyManifestBytes(ctx context.Context, kubeconfigPath string, data []byte) error {
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return fmt.Errorf("loading kubeconfig %s: %w", kubeconfigPath, err)
	}

	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("building dynamic client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("building discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))

	docs, err := splitYAMLDocuments(data)
	if err != nil {
		return fmt.Errorf("splitting manifest: %w", err)
	}

	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

	for _, doc := range docs {
		obj := &unstructured.Unstructured{}
		if _, _, err := decoder.Decode(doc, nil, obj); err != nil {
			return fmt.Errorf("decoding document: %w", err)
		}

		gvk := obj.GroupVersionKind()
		mapping, err := mapper.RESTMapping(schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}, gvk.Version)
		if err != nil {
			return fmt.Errorf("resolving REST mapping for %s: %w", gvk, err)
		}

		_, err = dynClient.Resource(mapping.Resource).Namespace(obj.GetNamespace()).Apply(
			ctx, obj.GetName(), obj, metav1.ApplyOptions{FieldManager: "clusterctl", Force: true},
		)
		if err != nil {
			return fmt.Errorf("applying %s/%s: %w", gvk.Kind, obj.GetName(), err)
		}
	}

	return nil
}
