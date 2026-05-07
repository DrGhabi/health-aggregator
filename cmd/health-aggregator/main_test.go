package main

import (
	"flag"
	"os"
	"testing"
)

func TestMainFlags(t *testing.T) {
	// Backup and restore os.Args and flag.CommandLine
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	oldCommandLine := flag.CommandLine
	defer func() { flag.CommandLine = oldCommandLine }()
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	os.Args = []string{"cmd", "--config", "test.yaml"}
	configPath := flag.String("config", "", "test")
	flag.Parse()

	if *configPath != "test.yaml" {
		t.Errorf("Expected config path test.yaml, got %s", *configPath)
	}
}
