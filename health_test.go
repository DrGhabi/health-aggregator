package health

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestAggregator_Report(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-deploy", Namespace: "default"},
			Status: appsv1.DeploymentStatus{
				Replicas:          3,
				AvailableReplicas: 2,
			},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-rs", Namespace: "default"},
			Status: appsv1.ReplicaSetStatus{
				Replicas:      3,
				ReadyReplicas: 3,
			},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "default"},
			Status: batchv1.JobStatus{
				Succeeded: 1,
			},
		},
	)

	var buf bytes.Buffer
	agg := NewAggregator(clientset, "default")
	agg.Logger.SetOutput(&buf)
	agg.Logger.SetFlags(0)

	agg.Report(context.Background())

	output := buf.String()
	expectedSubstrings := []string{
		"Health Summary:",
		"- Pods: 1",
		"test-pod: Running",
		"- Deployments: 1",
		"test-deploy: 2/3 available",
		"- ReplicaSets: 1",
		"test-rs: 3/3 replicas",
		"- Jobs: 1",
		"test-job: Succeeded",
	}

	for _, s := range expectedSubstrings {
		if !strings.Contains(output, s) {
			t.Errorf("Expected output to contain %q, but it didn't.\nOutput:\n%s", s, output)
		}
	}
}

func TestAggregator_Report_NoConfig(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-deploy", Namespace: "default"},
			Status: appsv1.DeploymentStatus{
				Replicas:          1,
				AvailableReplicas: 1,
			},
		},
		&appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-rs", Namespace: "default"},
			Status: appsv1.ReplicaSetStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
		&batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "default"},
			Status: batchv1.JobStatus{
				Succeeded: 1,
			},
		},
	)

	var buf bytes.Buffer
	agg := NewAggregator(clientset, "default")
	agg.Logger.SetOutput(&buf)
	agg.Logger.SetFlags(0)

	agg.Report(context.Background())

	output := buf.String()
	expectedSubstrings := []string{
		"test-pod: Running",
		"test-deploy: 1/1 available",
		"test-rs: 1/1 replicas",
		"test-job: Succeeded",
	}

	for _, s := range expectedSubstrings {
		if !strings.Contains(output, s) {
			t.Errorf("Expected output to contain %q, but it didn't.\nOutput:\n%s", s, output)
		}
	}
}

func TestGetConfig_Impersonation(t *testing.T) {
	t.Setenv("SERVICE_ACCOUNT_NAME", "test-sa")
	t.Setenv("NAMESPACE", "test-ns")
	t.Setenv("KUBECONFIG", "non-existent")
	_, _ = GetConfig()
}

func TestGetConfig_HomeDir(t *testing.T) {
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", "/tmp/fake-home")
	t.Setenv("USERPROFILE", `C:\tmp\fake-home`)
	_, _ = GetConfig()
}

func TestGetConfig_FileFound(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "kubeconfig")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	t.Setenv("KUBECONFIG", tmpFile.Name())
	_, _ = GetConfig()
}

func TestGetConfig(t *testing.T) {
	t.Setenv("KUBECONFIG", "non-existent-file")
	_, err := GetConfig()
	if err == nil {
		t.Error("Expected error when providing non-existent KUBECONFIG, but got nil")
	}
}

func TestGetNamespace(t *testing.T) {
	t.Run("FromEnv", func(t *testing.T) {
		t.Setenv("NAMESPACE", "my-ns")
		if got := GetNamespace(); got != "my-ns" {
			t.Errorf("GetNamespace() = %q, want %q", got, "my-ns")
		}
	})

	t.Run("Default", func(t *testing.T) {
		t.Setenv("NAMESPACE", "")
		got := GetNamespace()
		if got == "" {
			t.Error("GetNamespace() returned empty string")
		}
	})

	t.Run("FileFallbackFail", func(t *testing.T) {
		t.Setenv("NAMESPACE", "")
		// We expect this to return "default" on Windows because /var/run/... won't exist
		got := GetNamespace()
		if got != "default" && os.PathSeparator == '\\' {
			t.Errorf("Expected default on Windows, got %q", got)
		}
	})

	t.Run("EmptyEnv", func(t *testing.T) {
		t.Setenv("NAMESPACE", "")
		got := GetNamespace()
		if got == "" {
			t.Error("GetNamespace() returned empty string")
		}
	})
}
