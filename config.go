package health

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

type Config struct {
	PullInterval string       `yaml:"pullInterval,omitempty"`
	LogLevel     string       `yaml:"logLevel,omitempty"`
	Server       ServerConfig `yaml:"server"`
	Resources    Resources    `yaml:"resources"`
}

type ServerConfig struct {
	Main    EndpointConfig `yaml:"main"`
	Health  EndpointConfig `yaml:"health"`
	Metrics MetricsConfig  `yaml:"metrics"`
}

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path,omitempty"`
}

type EndpointConfig struct {
	Port int        `yaml:"port"`
	TLS  *TLSConfig `yaml:"tls,omitempty"`
}

type TLSConfig struct {
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
}

type Resources struct {
	Pods        []PodConfig        `yaml:"pods"`
	Deployments []DeploymentConfig `yaml:"deployments"`
	ReplicaSets []ReplicaSetConfig `yaml:"replicaSets"`
	Jobs        []JobConfig        `yaml:"jobs"`
}

type PodConfig struct {
	Name string `yaml:"name"`
	Min  *int   `yaml:"min,omitempty"`
	Max  *int   `yaml:"max,omitempty"`
}

type DeploymentConfig struct {
	Name     string `yaml:"name"`
	Replicas int    `yaml:"replicas"`
	Max      *int   `yaml:"max,omitempty"`
}

type ReplicaSetConfig struct {
	Name     string `yaml:"name"`
	Replicas int    `yaml:"replicas"`
	Max      *int   `yaml:"max,omitempty"`
}

type JobConfig struct {
	Name string `yaml:"name"`
}

func LoadConfig(r io.Reader) (*Config, error) {
	var cfg Config
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}
	return &cfg, nil
}
