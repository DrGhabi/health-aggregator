package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Aggregator represents a health status aggregator.
type Aggregator struct {
	Clientset kubernetes.Interface
	Namespace string
	Logger    *log.Logger
	Config    *Config

	// Prometheus metrics
	metricResourcesTotal   *prometheus.GaugeVec
	metricResourcesHealthy *prometheus.GaugeVec
	metricNamespaceHealth  *prometheus.GaugeVec
	metricsOnce            sync.Once
}

type logLevel int

const (
	levelDebug logLevel = iota
	levelInfo
	levelWarn
	levelError
)

var levelNames = map[string]logLevel{
	"debug": levelDebug,
	"info":  levelInfo,
	"warn":  levelWarn,
	"error": levelError,
}

func (a *Aggregator) getLogLevel() logLevel {
	if a.Config == nil || a.Config.LogLevel == "" {
		return levelInfo
	}
	if level, ok := levelNames[strings.ToLower(a.Config.LogLevel)]; ok {
		return level
	}
	return levelInfo
}

func (a *Aggregator) log(level logLevel, prefix, format string, v ...any) {
	if level >= a.getLogLevel() {
		a.Logger.Printf(prefix+format, v...)
	}
}

func (a *Aggregator) debug(format string, v ...any) {
	a.log(levelDebug, "[DEBUG] ", format, v...)
}

func (a *Aggregator) info(format string, v ...any) {
	a.log(levelInfo, "[INFO] ", format, v...)
}

func (a *Aggregator) warn(format string, v ...any) {
	a.log(levelWarn, "[WARN] ", format, v...)
}

func (a *Aggregator) error(format string, v ...any) {
	a.log(levelError, "[ERROR] ", format, v...)
}

// NewAggregator creates a new Aggregator.
func NewAggregator(clientset kubernetes.Interface, namespace string) *Aggregator {
	return &Aggregator{
		Clientset: clientset,
		Namespace: namespace,
		Logger:    log.New(os.Stdout, "", log.LstdFlags),
	}
}

func (a *Aggregator) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.debug("Access: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

// Start starts the aggregation loop.
func (a *Aggregator) Start(ctx context.Context) {
	interval := 10 * time.Second
	if a.Config != nil && a.Config.PullInterval != "" {
		if d, err := time.ParseDuration(a.Config.PullInterval); err == nil {
			interval = d
		} else {
			a.Logger.Printf("Error parsing pull interval %q, using default: %v", a.Config.PullInterval, err)
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	a.info("Starting health aggregator in namespace: %s (interval: %v)", a.Namespace, interval)

	// Immediate first run
	a.Report(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.Report(ctx)
		}
	}
}

// Report gathers and logs the health state of resources.
func (a *Aggregator) Report(ctx context.Context) {
	summary, health := a.checkHealth(ctx)
	a.info("\n" + summary)
	a.updateMetrics(health)
}

func (a *Aggregator) updateMetrics(overallHealth string) {
	if a.Config == nil || !a.Config.Server.Metrics.Enabled {
		return
	}

	a.initMetrics()

	healthVal := 1.0
	if overallHealth == "down" {
		healthVal = 0.0
	} else if overallHealth == "warning" {
		healthVal = 0.5
	}
	a.metricNamespaceHealth.WithLabelValues(a.Namespace).Set(healthVal)
}

func (a *Aggregator) initMetrics() {
	a.metricsOnce.Do(func() {
		a.metricResourcesTotal = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aggregup_resources_total",
				Help: "Total number of monitored resources",
			},
			[]string{"namespace", "resource_type"},
		)
		a.metricResourcesHealthy = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aggregup_resources_healthy",
				Help: "Number of healthy monitored resources",
			},
			[]string{"namespace", "resource_type"},
		)
		a.metricNamespaceHealth = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aggregup_namespace_health",
				Help: "Overall health of the namespace (1=up, 0.5=warning, 0=down)",
			},
			[]string{"namespace"},
		)

		err := prometheus.Register(a.metricResourcesTotal)
		if err != nil {
			if are, ok := errors.AsType[prometheus.AlreadyRegisteredError](err); ok {
				a.metricResourcesTotal = are.ExistingCollector.(*prometheus.GaugeVec)
			} else {
				panic(err)
			}
		}
		err = prometheus.Register(a.metricResourcesHealthy)
		if err != nil {
			if are, ok := errors.AsType[prometheus.AlreadyRegisteredError](err); ok {
				a.metricResourcesHealthy = are.ExistingCollector.(*prometheus.GaugeVec)
			} else {
				panic(err)
			}
		}
		err = prometheus.Register(a.metricNamespaceHealth)
		if err != nil {
			if are, ok := errors.AsType[prometheus.AlreadyRegisteredError](err); ok {
				a.metricNamespaceHealth = are.ExistingCollector.(*prometheus.GaugeVec)
			} else {
				panic(err)
			}
		}
	})
}

func (a *Aggregator) checkHealth(ctx context.Context) (string, string) {
	summary := "Health Summary:\n"
	overallHealth := "up"

	// Pods
	pods, err := a.Clientset.CoreV1().Pods(a.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		a.error("Error listing pods: %v", err)
	} else {
		summary += fmt.Sprintf("- Pods: %d\n", len(pods.Items))
		podMap := make(map[string][]corev1.Pod)
		for _, p := range pods.Items {
			podMap[p.Name] = append(podMap[p.Name], p)
		}

		if a.Config != nil {
			var total, healthy float64
			for _, pc := range a.Config.Resources.Pods {
				total++
				count := 0
				foundAny := false
				var phase corev1.PodPhase
				for name, ps := range podMap {
					if strings.HasPrefix(name, pc.Name) {
						count += len(ps)
						foundAny = true
						if len(ps) > 0 {
							phase = ps[0].Status.Phase
						}
					}
				}

				status := "OK"
				if pc.Min != nil && count < *pc.Min {
					status = "MISSING"
					overallHealth = "down"
				} else if pc.Max != nil && count > *pc.Max {
					status = "OVER LIMIT"
					if overallHealth != "down" {
						overallHealth = "warning"
					}
				} else {
					healthy++
				}

				if foundAny && pc.Min != nil && *pc.Min == 1 && count == 1 {
					summary += fmt.Sprintf("  * %s: %s [OK]\n", pc.Name, phase)
				} else {
					summary += fmt.Sprintf("  * %s: %d found [%s]\n", pc.Name, count, status)
				}
			}
			if a.Config.Server.Metrics.Enabled {
				a.initMetrics()
				a.metricResourcesTotal.WithLabelValues(a.Namespace, "pod").Set(total)
				a.metricResourcesHealthy.WithLabelValues(a.Namespace, "pod").Set(healthy)
			}
		} else {
			for name, ps := range podMap {
				if len(ps) > 0 {
					summary += fmt.Sprintf("  * %s: %s\n", name, ps[0].Status.Phase)
				}
			}
		}
	}

	// Deployments
	deps, err := a.Clientset.AppsV1().Deployments(a.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		a.error("Error listing deployments: %v", err)
	} else {
		summary += fmt.Sprintf("- Deployments: %d\n", len(deps.Items))
		depMap := make(map[string]appsv1.Deployment)
		for _, d := range deps.Items {
			depMap[d.Name] = d
		}

		if a.Config != nil {
			var total, healthy float64
			for _, dc := range a.Config.Resources.Deployments {
				total++
				if d, ok := depMap[dc.Name]; ok {
					status := "OK"
					if d.Status.AvailableReplicas < int32(dc.Replicas) {
						status = "MISMATCH"
						overallHealth = "down"
					} else if dc.Max != nil && d.Status.AvailableReplicas > int32(*dc.Max) {
						status = "OVER LIMIT"
						if overallHealth != "down" {
							overallHealth = "warning"
						}
					} else {
						healthy++
					}
					summary += fmt.Sprintf("  * %s: %d/%d available [%s]\n", d.Name, d.Status.AvailableReplicas, d.Status.Replicas, status)
				} else {
					summary += fmt.Sprintf("  * %s: [MISSING]\n", dc.Name)
					overallHealth = "down"
				}
			}
			if a.Config.Server.Metrics.Enabled {
				a.initMetrics()
				a.metricResourcesTotal.WithLabelValues(a.Namespace, "deployment").Set(total)
				a.metricResourcesHealthy.WithLabelValues(a.Namespace, "deployment").Set(healthy)
			}
		} else {
			for _, d := range depMap {
				summary += fmt.Sprintf("  * %s: %d/%d available\n", d.Name, d.Status.AvailableReplicas, d.Status.Replicas)
			}
		}
	}

	// ReplicaSets
	rss, err := a.Clientset.AppsV1().ReplicaSets(a.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		a.error("Error listing replicasets: %v", err)
	} else {
		summary += fmt.Sprintf("- ReplicaSets: %d\n", len(rss.Items))
		rsMap := make(map[string]appsv1.ReplicaSet)
		for _, rs := range rss.Items {
			rsMap[rs.Name] = rs
		}

		if a.Config != nil {
			var total, healthy float64
			for _, rc := range a.Config.Resources.ReplicaSets {
				total++
				if rs, ok := rsMap[rc.Name]; ok {
					status := "OK"
					if rs.Status.ReadyReplicas < int32(rc.Replicas) {
						status = "MISMATCH"
						overallHealth = "down"
					} else if rc.Max != nil && rs.Status.ReadyReplicas > int32(*rc.Max) {
						status = "OVER LIMIT"
						if overallHealth != "down" {
							overallHealth = "warning"
						}
					} else {
						healthy++
					}
					summary += fmt.Sprintf("  * %s: %d/%d replicas [%s]\n", rs.Name, rs.Status.ReadyReplicas, rs.Status.Replicas, status)
				} else {
					summary += fmt.Sprintf("  * %s: [MISSING]\n", rc.Name)
					overallHealth = "down"
				}
			}
			if a.Config.Server.Metrics.Enabled {
				a.initMetrics()
				a.metricResourcesTotal.WithLabelValues(a.Namespace, "replicaset").Set(total)
				a.metricResourcesHealthy.WithLabelValues(a.Namespace, "replicaset").Set(healthy)
			}
		} else {
			for _, rs := range rsMap {
				summary += fmt.Sprintf("  * %s: %d/%d replicas\n", rs.Name, rs.Status.ReadyReplicas, rs.Status.Replicas)
			}
		}
	}

	// Jobs
	jobs, err := a.Clientset.BatchV1().Jobs(a.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		a.error("Error listing jobs: %v", err)
	} else {
		summary += fmt.Sprintf("- Jobs: %d\n", len(jobs.Items))
		jobMap := make(map[string]batchv1.Job)
		for _, j := range jobs.Items {
			jobMap[j.Name] = j
		}

		if a.Config != nil {
			var total, healthy float64
			for _, jc := range a.Config.Resources.Jobs {
				total++
				if j, ok := jobMap[jc.Name]; ok {
					jobStatus := "Running"
					if j.Status.Succeeded > 0 {
						jobStatus = "Succeeded"
						healthy++
					} else if j.Status.Failed > 0 {
						jobStatus = "Failed"
						overallHealth = "down"
					}
					summary += fmt.Sprintf("  * %s: %s [OK]\n", j.Name, jobStatus)
				} else {
					summary += fmt.Sprintf("  * %s: [MISSING]\n", jc.Name)
					overallHealth = "down"
				}
			}
			if a.Config.Server.Metrics.Enabled {
				a.initMetrics()
				a.metricResourcesTotal.WithLabelValues(a.Namespace, "job").Set(total)
				a.metricResourcesHealthy.WithLabelValues(a.Namespace, "job").Set(healthy)
			}
		} else {
			for _, j := range jobMap {
				jobStatus := "Running"
				if j.Status.Succeeded > 0 {
					jobStatus = "Succeeded"
				} else if j.Status.Failed > 0 {
					jobStatus = "Failed"
				}
				summary += fmt.Sprintf("  * %s: %s\n", j.Name, jobStatus)
			}
		}
	}

	if overallHealth == "" {
		overallHealth = "up"
	}

	return summary, overallHealth
}

func (a *Aggregator) MainHandler(w http.ResponseWriter, r *http.Request) {
	_, health := a.checkHealth(r.Context())

	status := http.StatusOK
	if health == "down" {
		status = http.StatusServiceUnavailable
	} else if health == "warning" {
		status = 509 // Bandwidth Limit Exceeded (as requested)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"health": health})
}

func (a *Aggregator) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *Aggregator) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	a.initMetrics()
	promhttp.Handler().ServeHTTP(w, r)
}

// Serve starts the web servers as configured.
func (a *Aggregator) Serve(ctx context.Context) error {
	if a.Config == nil {
		return fmt.Errorf("configuration is required for serving")
	}

	errChan := make(chan error, 2)

	// Main Server
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /", a.MainHandler)
		server := &http.Server{
			Addr:    fmt.Sprintf(":%d", a.Config.Server.Main.Port),
			Handler: a.loggingMiddleware(mux),
		}
		a.info("Starting main server on port %d", a.Config.Server.Main.Port)

		var err error
		if a.Config.Server.Main.TLS != nil {
			err = server.ListenAndServeTLS(a.Config.Server.Main.TLS.CertFile, a.Config.Server.Main.TLS.KeyFile)
		} else {
			err = server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("main server error: %w", err)
		}
	}()

	// Health Server
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /health", a.HealthHandler)
		if a.Config.Server.Metrics.Enabled {
			path := a.Config.Server.Metrics.Path
			if path == "" {
				path = "/metrics"
			}
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			mux.HandleFunc("GET "+path, a.MetricsHandler)
			a.info("Prometheus metrics enabled on %s", path)
		}
		server := &http.Server{
			Addr:    fmt.Sprintf(":%d", a.Config.Server.Health.Port),
			Handler: a.loggingMiddleware(mux),
		}
		a.info("Starting health server on port %d", a.Config.Server.Health.Port)

		var err error
		if a.Config.Server.Health.TLS != nil {
			err = server.ListenAndServeTLS(a.Config.Server.Health.TLS.CertFile, a.Config.Server.Health.TLS.KeyFile)
		} else {
			err = server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("health server error: %w", err)
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		a.info("Shutting down servers...")
		return nil
	}
}

// GetNamespace returns the current namespace.
func GetNamespace() string {
	if ns := os.Getenv("NAMESPACE"); ns != "" {
		return ns
	}
	// Fallback for in-cluster
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		return string(data)
	}
	return "default"
}

// GetConfig returns the kubernetes config.
func GetConfig() (*rest.Config, error) {
	var config *rest.Config
	var err error

	// 1. Try KUBECONFIG env var or default kubeconfig file
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig != "" {
		if _, err := os.Stat(kubeconfig); err == nil {
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err == nil {
				log.Printf("Using kubernetes config from file: %s", kubeconfig)
			}
		}
	} else {
		// Try default location if HOME is set
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
			if _, err := os.Stat(kubeconfig); err == nil {
				config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
				if err == nil {
					log.Printf("Using kubernetes config from default file: %s", kubeconfig)
				}
			}
		}
	}

	// 2. Fallback to InClusterConfig
	if config == nil {
		config, err = rest.InClusterConfig()
		if err == nil {
			log.Println("Using in-cluster kubernetes config")
		} else {
			log.Printf("In-cluster config not found: %v", err)
		}
	}

	if err != nil {
		return nil, err
	}

	// 3. Handle service account name (impersonation)
	if saName := os.Getenv("SERVICE_ACCOUNT_NAME"); saName != "" {
		namespace := GetNamespace()
		if config != nil {
			config.Impersonate = rest.ImpersonationConfig{
				UserName: fmt.Sprintf("system:serviceaccount:%s:%s", namespace, saName),
			}
			log.Printf("Impersonating service account: %s in namespace: %s", saName, namespace)
		} else {
			log.Printf("Cannot impersonate %s: no kubernetes config available", saName)
		}
	}

	return config, nil
}
