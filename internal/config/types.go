package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("duration must be a string: %w", err)
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", raw, err)
	}
	d.Duration = parsed
	return nil
}

type Config struct {
	Version   int
	Name      string
	Chief     AgentConfig
	Agents    []AgentConfig
	Task      TaskConfig
	Budget    BudgetConfig
	Executors ExecutorConfig
	Runtime   RuntimeConfig
	Snapshot  Snapshot
	BaseDir   string
}

type AgentConfig struct {
	Version     int
	Role        string
	Model       ModelConfig
	Prompt      PromptConfig
	Tools       []string
	Context     ContextConfig
	Permissions Permissions
	SourcePath  string
}

type PromptConfig struct {
	Type    string `yaml:"type"`
	Path    string `yaml:"path"`
	Content string `yaml:"-"`
}

type ContextConfig struct {
	Strategy         string `yaml:"strategy"`
	PersistentMemory bool   `yaml:"persistent_memory"`
}

type Permissions struct {
	Network    string `yaml:"network"`
	Filesystem string `yaml:"filesystem"`
	Shell      string `yaml:"shell"`
}

type ModelConfig struct {
	Version      int
	Provider     string
	Model        string
	APIKeyEnv    string `yaml:"api_key_env"`
	BaseURL      string `yaml:"base_url"`
	Parameters   map[string]any
	Capabilities Capabilities
	Retry        RetryConfig
	Timeout      Duration
	RateLimit    RateLimitConfig `yaml:"rate_limit"`
	SourcePath   string          `yaml:"-"`
}

type Capabilities struct {
	Streaming           bool     `yaml:"streaming"`
	Tools               bool     `yaml:"tools"`
	StructuredOutputs   bool     `yaml:"structured_outputs"`
	SupportedParameters []string `yaml:"supported_parameters"`
	ReasoningEfforts    []string `yaml:"reasoning_efforts"`
}

type RetryConfig struct {
	MaxAttempts     int      `yaml:"max_attempts"`
	Backoff         string   `yaml:"backoff"`
	InitialInterval Duration `yaml:"initial_interval"`
	MaxInterval     Duration `yaml:"max_interval"`
}

type RateLimitConfig struct {
	RequestsPerMinute int `yaml:"requests_per_minute"`
}

type TaskConfig struct {
	Version            int
	Name               string
	Objective          string
	CompletionCriteria string
	Restrictions       string
	Deliverables       []string
	Verification       VerificationConfig
	SourcePath         string
}

type VerificationConfig struct {
	Version           int      `yaml:"version"`
	Type              string   `yaml:"-"`
	Command           []string `yaml:"command"`
	Timeout           Duration `yaml:"timeout"`
	RequiredArtifacts []string `yaml:"required_artifacts"`
}

type BudgetConfig struct {
	Version     int
	Money       MoneyConfig
	Providers   map[string]ProviderBudget
	Resources   ResourceBudget
	Termination TerminationConfig
}

type MoneyConfig struct {
	Currency               string
	TotalUSD               float64 `yaml:"total_usd"`
	SoftLimitUSD           float64 `yaml:"soft_limit_usd"`
	HardLimitUSD           float64 `yaml:"hard_limit_usd"`
	FinalizationReserveUSD float64 `yaml:"finalization_reserve_usd"`
}

type ProviderBudget struct {
	MaxUSD float64     `yaml:"max_usd"`
	Quota  QuotaConfig `yaml:"quota"`
}

type QuotaConfig struct {
	Type        string `yaml:"type"`
	MaxRequests int64  `yaml:"max_requests"`
}

type ResourceBudget struct {
	MaxRuntime        Duration `yaml:"max_runtime"`
	MaxParallelAgents int      `yaml:"max_parallel_agents"`
	MaxGPUHours       float64  `yaml:"max_gpu_hours"`
	MaxDiskGB         float64  `yaml:"max_disk_gb"`
}

type TerminationConfig struct {
	OnSoftLimit    string `yaml:"on_soft_limit"`
	OnHardLimit    string `yaml:"on_hard_limit"`
	OnCriterionMet string `yaml:"on_criterion_met"`
}

type ExecutorConfig struct {
	Version   int
	Default   string
	Executors map[string]ExecutorDefinition
}

type ExecutorDefinition struct {
	Type          string
	Enabled       *bool
	WorkspaceRoot string `yaml:"workspace_root"`
	Timeout       Duration
	Network       string
	MaxMemoryMB   int     `yaml:"max_memory_mb"`
	MaxCPUs       float64 `yaml:"max_cpus"`
}

type RuntimeConfig struct {
	Autonomous         bool
	MaxParallelTasks   int      `yaml:"max_parallel_tasks"`
	CheckpointInterval Duration `yaml:"checkpoint_interval"`
	ResumeOnRestart    bool     `yaml:"resume_on_restart"`
	DataDir            string   `yaml:"data_dir"`
	Listen             string
}

type Snapshot struct {
	Version   int               `json:"version"`
	CreatedAt time.Time         `json:"created_at"`
	Files     map[string]string `json:"files"`
	Digest    string            `json:"digest"`
}
