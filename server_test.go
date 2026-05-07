package health

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestAggregator_MainHandler(t *testing.T) {
	tests := []struct {
		name           string
		config         string
		resources      []any
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "All OK",
			config: `
resources:
  pods:
    - name: "pod-1"
      min: 1
`,
			resources: []any{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   `{"health":"up"}`,
		},
		{
			name: "Missing Resource (Down)",
			config: `
resources:
  pods:
    - name: "pod-1"
      min: 1
`,
			resources:      []any{},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   `{"health":"down"}`,
		},
		{
			name: "Mismatch Below Min (Down)",
			config: `
resources:
  deployments:
    - name: "dep-1"
      replicas: 2
`,
			resources: []any{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "dep-1", Namespace: "default"},
					Status: appsv1.DeploymentStatus{
						AvailableReplicas: 1,
						Replicas:          2,
					},
				},
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   `{"health":"down"}`,
		},
		{
			name: "Mismatch Above Max (Warning)",
			config: `
resources:
  pods:
    - name: "pod-1"
      max: 1
`,
			resources: []any{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-1-extra", Namespace: "default"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expectedStatus: 509,
			expectedBody:   `{"health":"warning"}`,
		},
		{
			name: "ReplicaSet OK",
			config: `
resources:
  replicaSets:
    - name: "rs-1"
      replicas: 1
`,
			resources: []any{
				&appsv1.ReplicaSet{
					ObjectMeta: metav1.ObjectMeta{Name: "rs-1", Namespace: "default"},
					Status: appsv1.ReplicaSetStatus{
						ReadyReplicas: 1,
						Replicas:      1,
					},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   `{"health":"up"}`,
		},
		{
			name: "ReplicaSet Mismatch (Down)",
			config: `
resources:
  replicaSets:
    - name: "rs-1"
      replicas: 2
`,
			resources: []any{
				&appsv1.ReplicaSet{
					ObjectMeta: metav1.ObjectMeta{Name: "rs-1", Namespace: "default"},
					Status: appsv1.ReplicaSetStatus{
						ReadyReplicas: 1,
						Replicas:      2,
					},
				},
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   `{"health":"down"}`,
		},
		{
			name: "ReplicaSet Over Limit (Warning)",
			config: `
resources:
  replicaSets:
    - name: "rs-1"
      replicas: 1
      max: 1
`,
			resources: []any{
				&appsv1.ReplicaSet{
					ObjectMeta: metav1.ObjectMeta{Name: "rs-1", Namespace: "default"},
					Status: appsv1.ReplicaSetStatus{
						ReadyReplicas: 2,
						Replicas:      2,
					},
				},
			},
			expectedStatus: 509,
			expectedBody:   `{"health":"warning"}`,
		},
		{
			name: "Job OK",
			config: `
resources:
  jobs:
    - name: "job-1"
`,
			resources: []any{
				&batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "default"},
					Status: batchv1.JobStatus{
						Succeeded: 1,
					},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   `{"health":"up"}`,
		},
		{
			name: "Job Failed (Down)",
			config: `
resources:
  jobs:
    - name: "job-1"
`,
			resources: []any{
				&batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{Name: "job-1", Namespace: "default"},
					Status: batchv1.JobStatus{
						Failed: 1,
					},
				},
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   `{"health":"down"}`,
		},
		{
			name: "Job Missing (Down)",
			config: `
resources:
  jobs:
    - name: "job-1"
`,
			resources:      []any{},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   `{"health":"down"}`,
		},
		{
			name: "Deployment Missing (Down)",
			config: `
resources:
  deployments:
    - name: "dep-1"
      replicas: 1
`,
			resources:      []any{},
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   `{"health":"down"}`,
		},
		{
			name: "Pod Prefix OK",
			config: `
resources:
  pods:
    - name: "app-"
      min: 2
`,
			resources: []any{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "app-1", Namespace: "default"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "app-2", Namespace: "default"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   `{"health":"up"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadConfig(strings.NewReader(tt.config))
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			var k8sObjs []runtime.Object
			for _, obj := range tt.resources {
				k8sObjs = append(k8sObjs, obj.(runtime.Object))
			}
			clientset := fake.NewSimpleClientset(k8sObjs...)
			agg := NewAggregator(clientset, "default")
			agg.Config = cfg

			req, err := http.NewRequestWithContext(t.Context(), "GET", "/", nil)
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			handler := http.HandlerFunc(agg.MainHandler)

			handler.ServeHTTP(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v", status, tt.expectedStatus)
			}

			var got map[string]string
			if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}

			var want map[string]string
			if err := json.Unmarshal([]byte(tt.expectedBody), &want); err != nil {
				t.Fatal(err)
			}

			if got["health"] != want["health"] {
				t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), tt.expectedBody)
			}
		})
	}
}

func TestAggregator_HealthHandler(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	agg := NewAggregator(clientset, "default")

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(agg.HealthHandler)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	expected := `{"status":"ok"}`
	if strings.TrimSpace(rr.Body.String()) != expected {
		t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), expected)
	}
}

func TestAggregator_Serve_NoConfig(t *testing.T) {
	agg := NewAggregator(fake.NewSimpleClientset(), "default")
	err := agg.Serve(context.Background())
	if err == nil {
		t.Error("Expected error when serving without config, got nil")
	}
}

func TestAggregator_Serve_Shutdown(t *testing.T) {
	agg := NewAggregator(fake.NewSimpleClientset(), "default")
	agg.Config = &Config{
		Server: ServerConfig{
			Main:   EndpointConfig{Port: 0}, // Random port
			Health: EndpointConfig{Port: 0}, // Random port
		},
	}
	agg.Logger.SetOutput(io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)
	go func() {
		errChan <- agg.Serve(ctx)
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("Serve returned error on shutdown: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("Serve did not shut down in time")
	}
}

func TestAggregator_Serve_TLSError(t *testing.T) {
	agg := NewAggregator(fake.NewSimpleClientset(), "default")
	agg.Config = &Config{
		Server: ServerConfig{
			Main: EndpointConfig{
				Port: 0,
				TLS: &TLSConfig{
					CertFile: "non-existent",
					KeyFile:  "non-existent",
				},
			},
			Health: EndpointConfig{Port: 0},
		},
	}
	agg.Logger.SetOutput(io.Discard)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := agg.Serve(ctx)
	if err == nil {
		t.Error("Expected error when serving with non-existent TLS files, got nil")
	}
	if !strings.Contains(err.Error(), "main server error") {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestAggregator_Start(t *testing.T) {
	agg := NewAggregator(fake.NewSimpleClientset(), "default")
	var buf bytes.Buffer
	agg.Logger = log.New(&buf, "", 0)

	ctx, cancel := context.WithCancel(context.Background())
	go agg.Start(ctx)

	// Give it a moment to run at least once
	time.Sleep(100 * time.Millisecond)
	cancel()

	output := buf.String()
	if !strings.Contains(output, "Starting health aggregator") {
		t.Error("Start() did not log starting message")
	}
	if !strings.Contains(output, "(interval: 10s)") {
		t.Error("Start() did not log default interval")
	}
	if !strings.Contains(output, "Health Summary:") {
		t.Error("Start() did not run Report at least once")
	}
}

func TestAggregator_MetricsHandler(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	agg := NewAggregator(clientset, "default")
	agg.Config = &Config{
		Server: ServerConfig{
			Metrics: MetricsConfig{
				Enabled: true,
			},
		},
	}

	req, err := http.NewRequest("GET", "/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(agg.MetricsHandler)

	// Ensure metrics are initialized for the test
	agg.initMetrics()

	// Set some values so we have custom metrics in the output
	agg.metricNamespaceHealth.WithLabelValues("default").Set(1)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	output := rr.Body.String()
	if !strings.Contains(output, "aggregup_namespace_health") {
		t.Errorf("metrics output does not contain expected metric: %s", output)
	}
}

func TestAggregator_Report_WithMetrics(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)
	agg := NewAggregator(clientset, "default")
	agg.Config = &Config{
		Server: ServerConfig{
			Metrics: MetricsConfig{
				Enabled: true,
			},
		},
		Resources: Resources{
			Pods: []PodConfig{
				{Name: "test-pod", Min: new(1)},
			},
		},
	}
	agg.Logger.SetOutput(io.Discard)

	agg.Report(t.Context())

	req, _ := http.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	agg.MetricsHandler(rr, req)

	output := rr.Body.String()
	expectedMetrics := []string{
		`aggregup_namespace_health{namespace="default"} 1`,
		`aggregup_resources_total{namespace="default",resource_type="pod"} 1`,
		`aggregup_resources_healthy{namespace="default",resource_type="pod"} 1`,
	}

	for _, m := range expectedMetrics {
		if !strings.Contains(output, m) {
			t.Errorf("Expected metrics to contain %q, but it didn't.\nOutput:\n%s", m, output)
		}
	}
}

func TestAggregator_Start_CustomInterval(t *testing.T) {
	agg := NewAggregator(fake.NewSimpleClientset(), "default")
	agg.Config = &Config{PullInterval: "1s"}
	var buf bytes.Buffer
	agg.Logger = log.New(&buf, "", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	go agg.Start(ctx)

	// Wait for context to finish or timeout
	<-ctx.Done()

	output := buf.String()
	if !strings.Contains(output, "(interval: 1s)") {
		t.Errorf("Start() did not log custom interval: %s", output)
	}

	// Should have run at least 3 times (0s, 1s, 2s)
	count := strings.Count(output, "Health Summary:")
	if count < 3 {
		t.Errorf("Expected at least 3 reports, got %d. Output:\n%s", count, output)
	}
}

func TestAggregator_LogLevels(t *testing.T) {
	tests := []struct {
		name            string
		configuredLevel string
		logAction       func(a *Aggregator)
		expectedLog     string
		unexpectedLog   string
	}{
		{
			name:            "Debug level shows debug logs",
			configuredLevel: "debug",
			logAction:       func(a *Aggregator) { a.debug("test debug") },
			expectedLog:     "[DEBUG] test debug",
		},
		{
			name:            "Info level hides debug logs",
			configuredLevel: "info",
			logAction:       func(a *Aggregator) { a.debug("test debug") },
			unexpectedLog:   "[DEBUG] test debug",
		},
		{
			name:            "Info level shows info logs",
			configuredLevel: "info",
			logAction:       func(a *Aggregator) { a.info("test info") },
			expectedLog:     "[INFO] test info",
		},
		{
			name:            "Warn level hides info logs",
			configuredLevel: "warn",
			logAction:       func(a *Aggregator) { a.info("test info") },
			unexpectedLog:   "[INFO] test info",
		},
		{
			name:            "Warn level shows warn logs",
			configuredLevel: "warn",
			logAction:       func(a *Aggregator) { a.warn("test warn") },
			expectedLog:     "[WARN] test warn",
		},
		{
			name:            "Error level hides warn logs",
			configuredLevel: "error",
			logAction:       func(a *Aggregator) { a.warn("test warn") },
			unexpectedLog:   "[WARN] test warn",
		},
		{
			name:            "Error level shows error logs",
			configuredLevel: "error",
			logAction:       func(a *Aggregator) { a.error("test error") },
			expectedLog:     "[ERROR] test error",
		},
		{
			name:            "Default level is info",
			configuredLevel: "",
			logAction:       func(a *Aggregator) { a.debug("test debug"); a.info("test info") },
			expectedLog:     "[INFO] test info",
			unexpectedLog:   "[DEBUG] test debug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			agg := NewAggregator(fake.NewSimpleClientset(), "default")
			agg.Logger = log.New(&buf, "", 0)
			agg.Config = &Config{LogLevel: tt.configuredLevel}

			tt.logAction(agg)

			output := buf.String()
			if tt.expectedLog != "" && !strings.Contains(output, tt.expectedLog) {
				t.Errorf("Expected log output to contain %q, got %q", tt.expectedLog, output)
			}
			if tt.unexpectedLog != "" && strings.Contains(output, tt.unexpectedLog) {
				t.Errorf("Expected log output NOT to contain %q, but it did. Output: %q", tt.unexpectedLog, output)
			}
		})
	}
}
