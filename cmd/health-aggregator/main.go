package main

import (
	"context"
	"flag"
	"health-aggregator"
	"log"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/kubernetes"
)

func main() {
	configPath := flag.String("config", os.Getenv("CONFIG_PATH"), "Path to the configuration file")
	flag.Parse()

	k8sConfig, err := health.GetConfig()
	if err != nil {
		log.Fatalf("Error getting kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		log.Fatalf("Error creating kubernetes clientset: %v", err)
	}

	namespace := health.GetNamespace()
	agg := health.NewAggregator(clientset, namespace)

	if *configPath != "" {
		f, err := os.Open(*configPath)
		if err != nil {
			log.Fatalf("Error opening config file: %v", err)
		}
		defer f.Close()

		cfg, err := health.LoadConfig(f)
		if err != nil {
			log.Fatalf("Error loading config: %v", err)
		}
		agg.Config = cfg
		agg.Logger.Printf("Loaded configuration from: %s", *configPath)
	}

	if envInterval := os.Getenv("PULL_INTERVAL"); envInterval != "" {
		if agg.Config == nil {
			agg.Config = &health.Config{}
		}
		agg.Config.PullInterval = envInterval
	}

	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		if agg.Config == nil {
			agg.Config = &health.Config{}
		}
		agg.Config.LogLevel = logLevel
	} else if os.Getenv("DEBUG") == "true" {
		if agg.Config == nil {
			agg.Config = &health.Config{}
		}
		agg.Config.LogLevel = "debug"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Run periodic reporting in background
	go agg.Start(ctx)

	// Run web servers
	if agg.Config != nil {
		if err := agg.Serve(ctx); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else {
		agg.Logger.Println("No configuration provided, web servers not started. Running in reporting-only mode.")
		<-ctx.Done()
	}
}
