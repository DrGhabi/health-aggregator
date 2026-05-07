package health

import (
	"bytes"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestAggregator_ReportWithConfig(t *testing.T) {
	configYAML := `
resources:
  pods:
    - name: "test-pod"
      min: 1
  deployments:
    - name: "test-deploy"
      replicas: 3
`
	config, err := LoadConfig(strings.NewReader(configYAML))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	clientset := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-deploy", Namespace: "default"},
			Status: appsv1.DeploymentStatus{
				Replicas:          3,
				AvailableReplicas: 3,
			},
		},
	)

	var buf bytes.Buffer
	agg := NewAggregator(clientset, "default")
	agg.Config = config
	agg.Logger.SetOutput(&buf)
	agg.Logger.SetFlags(0)

	agg.Report(t.Context())

	output := buf.String()
	expectedSubstrings := []string{
		"test-pod: Running [OK]",
		"test-deploy: 3/3 available [OK]",
	}

	for _, s := range expectedSubstrings {
		if !strings.Contains(output, s) {
			t.Errorf("Expected output to contain %q, but it didn't.\nOutput:\n%s", s, output)
		}
	}
}

func TestLoadConfig_Error(t *testing.T) {
	invalidYAML := `
resources:
  pods:
    - name: 123
      min: "abc"
`
	_, err := LoadConfig(strings.NewReader(invalidYAML))
	if err == nil {
		t.Error("Expected error when loading invalid YAML, but got nil")
	}
}

func TestLoadConfig_PullInterval(t *testing.T) {
	configYAML := `
pullInterval: "5s"
resources:
  pods: []
`
	config, err := LoadConfig(strings.NewReader(configYAML))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if config.PullInterval != "5s" {
		t.Errorf("Expected PullInterval to be '5s', got %q", config.PullInterval)
	}
}

func TestAggregator_ReportWithMissingResources(t *testing.T) {
	configYAML := `
resources:
  pods:
    - name: "missing-pod"
      min: 1
  deployments:
    - name: "missing-deploy"
      replicas: 1
`
	config, err := LoadConfig(strings.NewReader(configYAML))
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	clientset := fake.NewSimpleClientset() // Empty

	var buf bytes.Buffer
	agg := NewAggregator(clientset, "default")
	agg.Config = config
	agg.Logger.SetOutput(&buf)
	agg.Logger.SetFlags(0)

	agg.Report(t.Context())

	output := buf.String()
	expectedSubstrings := []string{
		"missing-pod: 0 found [MISSING]",
		"missing-deploy: [MISSING]",
	}

	for _, s := range expectedSubstrings {
		if !strings.Contains(output, s) {
			t.Errorf("Expected output to contain %q, but it didn't.\nOutput:\n%s", s, output)
		}
	}
}
