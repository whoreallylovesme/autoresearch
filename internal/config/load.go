package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type mainFile struct {
	Version   int           `yaml:"version"`
	Name      string        `yaml:"name"`
	Chief     agentRef      `yaml:"chief"`
	Workers   []workerRef   `yaml:"workers"`
	Task      string        `yaml:"task"`
	Budget    string        `yaml:"budget"`
	Executors string        `yaml:"executors"`
	Runtime   RuntimeConfig `yaml:"runtime"`
}

type agentRef struct {
	Config string `yaml:"config"`
}

type workerRef struct {
	Config    string `yaml:"config"`
	Headcount int    `yaml:"headcount"`
}

type agentFile struct {
	Version     int           `yaml:"version"`
	Role        string        `yaml:"role"`
	Model       string        `yaml:"model"`
	Prompt      PromptConfig  `yaml:"prompt"`
	Tools       []string      `yaml:"tools"`
	Context     ContextConfig `yaml:"context"`
	Permissions Permissions   `yaml:"permissions"`
}

type modelFile struct {
	Version      int              `yaml:"version"`
	Provider     string           `yaml:"provider"`
	Model        string           `yaml:"model"`
	APIKeyEnv    string           `yaml:"api_key_env"`
	BaseURL      string           `yaml:"base_url"`
	Parameters   map[string]any   `yaml:"parameters"`
	Capabilities Capabilities     `yaml:"capabilities"`
	Retry        RetryConfig      `yaml:"retry"`
	Timeout      Duration         `yaml:"timeout"`
	RateLimit    RateLimitConfig  `yaml:"rate_limit"`
	Pricing      *PricingConfig   `yaml:"pricing"`
	Estimation   EstimationConfig `yaml:"estimation"`
}

type taskFile struct {
	Version        int              `yaml:"version"`
	Name           string           `yaml:"name"`
	Task           string           `yaml:"task"`
	CriterionOfEnd string           `yaml:"criterion_of_end"`
	Restrictions   string           `yaml:"restrictions"`
	Deliverables   []string         `yaml:"deliverables"`
	Verification   taskVerification `yaml:"verification"`
}

type taskVerification struct {
	Type   string `yaml:"type"`
	Config string `yaml:"config"`
}

type budgetFile struct {
	Version     int                       `yaml:"version"`
	Money       MoneyConfig               `yaml:"money"`
	Providers   map[string]ProviderBudget `yaml:"providers"`
	Resources   ResourceBudget            `yaml:"resources"`
	Termination TerminationConfig         `yaml:"termination"`
}

type executorFile struct {
	Version   int                           `yaml:"version"`
	Default   string                        `yaml:"default"`
	Executors map[string]ExecutorDefinition `yaml:"executors"`
}

type loader struct {
	stack   map[string]bool
	files   map[string]string
	baseDir string
}

func Load(path string) (*Config, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	l := &loader{stack: map[string]bool{}, files: map[string]string{}, baseDir: filepath.Dir(absolute)}
	if err := l.enter(absolute); err != nil {
		return nil, err
	}
	defer delete(l.stack, absolute)
	var raw mainFile
	if err := l.decode(absolute, &raw); err != nil {
		return nil, err
	}
	if raw.Version != 1 || strings.TrimSpace(raw.Name) == "" {
		return nil, fmt.Errorf("main configuration requires version: 1 and a non-empty name")
	}
	cfg := &Config{Version: raw.Version, Name: raw.Name, Runtime: raw.Runtime, BaseDir: l.baseDir}
	chief, err := l.loadAgent(l.resolve(l.baseDir, raw.Chief.Config))
	if err != nil {
		return nil, fmt.Errorf("chief: %w", err)
	}
	if chief.Role != "chief-scientist" {
		return nil, fmt.Errorf("chief must use chief-scientist role, got %q", chief.Role)
	}
	cfg.Chief = chief
	for index, worker := range raw.Workers {
		if worker.Headcount < 1 {
			return nil, fmt.Errorf("workers[%d].headcount must be positive", index)
		}
		agent, err := l.loadAgent(l.resolve(l.baseDir, worker.Config))
		if err != nil {
			return nil, fmt.Errorf("workers[%d]: %w", index, err)
		}
		if agent.Role == "chief-scientist" {
			return nil, fmt.Errorf("workers[%d]: chief scientist cannot be a worker", index)
		}
		for range worker.Headcount {
			cfg.Agents = append(cfg.Agents, agent)
		}
	}
	if len(cfg.Agents) == 0 {
		return nil, errors.New("at least one worker is required")
	}
	cfg.Task, err = l.loadTask(l.resolve(l.baseDir, raw.Task))
	if err != nil {
		return nil, fmt.Errorf("task: %w", err)
	}
	cfg.Budget, err = l.loadBudget(l.resolve(l.baseDir, raw.Budget))
	if err != nil {
		return nil, fmt.Errorf("budget: %w", err)
	}
	cfg.Executors, err = l.loadExecutors(l.resolve(l.baseDir, raw.Executors))
	if err != nil {
		return nil, fmt.Errorf("executors: %w", err)
	}
	if err := validateRuntime(&cfg.Runtime, l.baseDir); err != nil {
		return nil, err
	}
	cfg.Snapshot = Snapshot{Version: 1, CreatedAt: time.Now().UTC(), Files: l.files}
	cfg.Snapshot.Digest = snapshotDigest(l.files)
	return cfg, nil
}

func (l *loader) loadAgent(path string) (AgentConfig, error) {
	if err := l.enter(path); err != nil {
		return AgentConfig{}, err
	}
	defer delete(l.stack, path)
	var raw agentFile
	if err := l.decode(path, &raw); err != nil {
		return AgentConfig{}, err
	}
	agent := AgentConfig{Version: raw.Version, Role: raw.Role, Prompt: raw.Prompt, Tools: raw.Tools, Context: raw.Context, Permissions: raw.Permissions, SourcePath: path}
	if raw.Version != 1 || !knownRole(raw.Role) {
		return AgentConfig{}, fmt.Errorf("%s: unsupported version or role %q", path, raw.Role)
	}
	model, err := l.loadModel(l.resolve(filepath.Dir(path), raw.Model))
	if err != nil {
		return AgentConfig{}, err
	}
	agent.Model = model
	promptPath := raw.Prompt.Path
	if raw.Prompt.Type == "default" && promptPath == "" {
		promptPath = filepath.Join("..", "..", "prompts", raw.Role+".md")
	}
	if raw.Prompt.Type != "default" && raw.Prompt.Type != "custom" {
		return AgentConfig{}, fmt.Errorf("%s: prompt.type must be default or custom", path)
	}
	content, err := l.text(l.resolve(filepath.Dir(path), promptPath))
	if err != nil {
		return AgentConfig{}, fmt.Errorf("prompt: %w", err)
	}
	agent.Prompt.Content = content
	if err := validateAgent(agent); err != nil {
		return AgentConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	return agent, nil
}

func (l *loader) loadModel(path string) (ModelConfig, error) {
	if err := l.enter(path); err != nil {
		return ModelConfig{}, err
	}
	defer delete(l.stack, path)
	var raw modelFile
	if err := l.decode(path, &raw); err != nil {
		return ModelConfig{}, err
	}
	model := ModelConfig{Version: raw.Version, Provider: raw.Provider, Model: raw.Model, APIKeyEnv: raw.APIKeyEnv, BaseURL: raw.BaseURL, Parameters: raw.Parameters, Capabilities: raw.Capabilities, Retry: raw.Retry, Timeout: raw.Timeout, RateLimit: raw.RateLimit, Pricing: raw.Pricing, Estimation: raw.Estimation, SourcePath: path}
	if err := validateModel(&model); err != nil {
		return ModelConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	return model, nil
}

func (l *loader) loadTask(path string) (TaskConfig, error) {
	if err := l.enter(path); err != nil {
		return TaskConfig{}, err
	}
	defer delete(l.stack, path)
	var raw taskFile
	if err := l.decode(path, &raw); err != nil {
		return TaskConfig{}, err
	}
	if raw.Version != 1 || strings.TrimSpace(raw.Name) == "" {
		return TaskConfig{}, errors.New("task requires version: 1 and a name")
	}
	base := filepath.Dir(path)
	objective, err := l.text(l.resolve(base, raw.Task))
	if err != nil {
		return TaskConfig{}, err
	}
	criteria, err := l.text(l.resolve(base, raw.CriterionOfEnd))
	if err != nil {
		return TaskConfig{}, err
	}
	restrictions, err := l.text(l.resolve(base, raw.Restrictions))
	if err != nil {
		return TaskConfig{}, err
	}
	verification := VerificationConfig{Type: raw.Verification.Type}
	if raw.Verification.Config != "" {
		if err := l.decode(l.resolve(base, raw.Verification.Config), &verification); err != nil {
			return TaskConfig{}, err
		}
		verification.Type = raw.Verification.Type
	}
	task := TaskConfig{Version: raw.Version, Name: raw.Name, Objective: objective, CompletionCriteria: criteria, Restrictions: restrictions, Deliverables: raw.Deliverables, Verification: verification, SourcePath: path}
	if len(task.Deliverables) == 0 {
		return TaskConfig{}, errors.New("at least one deliverable is required")
	}
	if verification.Type != "custom-command" || len(verification.Command) == 0 || verification.Timeout.Duration <= 0 {
		return TaskConfig{}, errors.New("custom-command verification requires a command and positive timeout")
	}
	return task, nil
}

func (l *loader) loadBudget(path string) (BudgetConfig, error) {
	if err := l.enter(path); err != nil {
		return BudgetConfig{}, err
	}
	defer delete(l.stack, path)
	var raw budgetFile
	if err := l.decode(path, &raw); err != nil {
		return BudgetConfig{}, err
	}
	budget := BudgetConfig{Version: raw.Version, Money: raw.Money, Providers: raw.Providers, Resources: raw.Resources, Termination: raw.Termination}
	if raw.Version != 1 || raw.Money.Currency != "USD" {
		return BudgetConfig{}, errors.New("budget requires version: 1 and USD currency")
	}
	money := raw.Money
	if !nonNegative(money.TotalUSD) || !nonNegative(money.SoftLimitUSD) || !nonNegative(money.HardLimitUSD) || !nonNegative(money.FinalizationReserveUSD) {
		return BudgetConfig{}, errors.New("monetary limits must be finite and non-negative")
	}
	if money.SoftLimitUSD > money.HardLimitUSD {
		return BudgetConfig{}, errors.New("soft_limit_usd cannot exceed hard_limit_usd")
	}
	if money.HardLimitUSD > money.TotalUSD || money.FinalizationReserveUSD > money.TotalUSD {
		return BudgetConfig{}, errors.New("hard and finalization limits cannot exceed total_usd")
	}
	if raw.Resources.MaxRuntime.Duration <= 0 || raw.Resources.MaxParallelAgents < 1 || raw.Resources.MaxDiskGB <= 0 || !nonNegative(raw.Resources.MaxGPUHours) {
		return BudgetConfig{}, errors.New("resource limits must be positive")
	}
	for provider, limit := range raw.Providers {
		if !nonNegative(limit.MaxUSD) || limit.Quota.MaxRequests < 1 {
			return BudgetConfig{}, fmt.Errorf("provider %s has invalid limits", provider)
		}
	}
	if raw.Termination.OnSoftLimit != "finalize" || raw.Termination.OnHardLimit != "stop" || raw.Termination.OnCriterionMet != "finalize" {
		return BudgetConfig{}, errors.New("unsupported termination policy")
	}
	return budget, nil
}

func (l *loader) loadExecutors(path string) (ExecutorConfig, error) {
	if err := l.enter(path); err != nil {
		return ExecutorConfig{}, err
	}
	defer delete(l.stack, path)
	var raw executorFile
	if err := l.decode(path, &raw); err != nil {
		return ExecutorConfig{}, err
	}
	if raw.Version != 1 || len(raw.Executors) == 0 {
		return ExecutorConfig{}, errors.New("executors require version: 1 and at least one definition")
	}
	selected, ok := raw.Executors[raw.Default]
	if !ok || (selected.Enabled != nil && !*selected.Enabled) || selected.Timeout.Duration <= 0 {
		return ExecutorConfig{}, fmt.Errorf("default executor %q is missing, disabled, or has no timeout", raw.Default)
	}
	for name, executor := range raw.Executors {
		if executor.Type != "mock" && executor.Type != "local" && executor.Type != "docker" && executor.Type != "ssh" && executor.Type != "slurm" {
			return ExecutorConfig{}, fmt.Errorf("executor %s has unsupported type %q", name, executor.Type)
		}
	}
	return ExecutorConfig{Version: raw.Version, Default: raw.Default, Executors: raw.Executors}, nil
}

func (l *loader) enter(path string) error {
	if l.stack[path] {
		return fmt.Errorf("circular configuration dependency at %s", path)
	}
	l.stack[path] = true
	return nil
}

func (l *loader) decode(path string, destination any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	l.record(path, content)
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	if err := checkDuplicateKeys(&document); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	decoder = yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode %s: multiple YAML documents are not supported", path)
	}
	return nil
}

func (l *loader) text(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if strings.TrimSpace(string(content)) == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	l.record(path, content)
	return string(content), nil
}

func (l *loader) record(path string, content []byte) {
	sum := sha256.Sum256(content)
	l.files[path] = hex.EncodeToString(sum[:])
}

func (l *loader) resolve(base, reference string) string {
	if strings.TrimSpace(reference) == "" {
		return ""
	}
	if filepath.IsAbs(reference) {
		return filepath.Clean(reference)
	}
	return filepath.Clean(filepath.Join(base, reference))
}

func checkDuplicateKeys(node *yaml.Node) error {
	if node.Kind == yaml.DocumentNode {
		for _, child := range node.Content {
			if err := checkDuplicateKeys(child); err != nil {
				return err
			}
		}
		return nil
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index].Value
			if seen[key] {
				return fmt.Errorf("duplicate mapping key %q at line %d", key, node.Content[index].Line)
			}
			seen[key] = true
		}
	}
	for _, child := range node.Content {
		if err := checkDuplicateKeys(child); err != nil {
			return err
		}
	}
	return nil
}

func validateAgent(agent AgentConfig) error {
	if len(agent.Tools) == 0 || agent.Context.Strategy != "hierarchical-summary" || !agent.Context.PersistentMemory {
		return errors.New("agent requires tools and persistent hierarchical-summary context")
	}
	if agent.Permissions.Filesystem != "workspace-only" || !slices.Contains([]string{"none", "restricted"}, agent.Permissions.Network) || !slices.Contains([]string{"false", "sandboxed"}, agent.Permissions.Shell) {
		return errors.New("agent permissions exceed supported autonomy boundary")
	}
	seen := map[string]bool{}
	for _, tool := range agent.Tools {
		if seen[tool] || !toolAllowed(agent.Role, tool) {
			return fmt.Errorf("tool %q is duplicated or incompatible with role %s", tool, agent.Role)
		}
		seen[tool] = true
	}
	if slices.Contains(agent.Tools, "run_test") && agent.Permissions.Shell != "sandboxed" {
		return errors.New("run_test requires sandboxed shell permission")
	}
	return nil
}

func validateModel(model *ModelConfig) error {
	if model.Version != 1 || strings.TrimSpace(model.Model) == "" || strings.HasPrefix(model.Model, "replace-with-") {
		return errors.New("model requires version: 1 and a configured model identifier")
	}
	allowed := []string{"max_output_tokens"}
	switch model.Provider {
	case "mock":
		allowed = append(allowed, "temperature")
	case "openai":
		model.BaseURL = defaultString(model.BaseURL, "https://api.openai.com/v1")
	case "kimi":
		model.BaseURL = defaultString(model.BaseURL, "https://api.moonshot.ai/v1")
	case "openai-compatible":
		allowed = append(allowed, "temperature")
	default:
		return fmt.Errorf("unsupported provider %q", model.Provider)
	}
	if model.Provider != "mock" {
		parsed, err := url.Parse(model.BaseURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return errors.New("base_url must be a valid HTTP(S) URL")
		}
	}
	if (model.Provider == "openai" || model.Provider == "kimi") && model.APIKeyEnv == "" {
		return errors.New("api_key_env is required for authenticated providers")
	}
	if model.APIKeyEnv != "" && strings.ContainsAny(model.APIKeyEnv, "= \t\n") {
		return errors.New("api_key_env must name an environment variable, not contain a secret")
	}
	if len(model.Capabilities.SupportedParameters) > 0 {
		allowed = model.Capabilities.SupportedParameters
	}
	for name, value := range model.Parameters {
		if !slices.Contains(allowed, name) {
			return fmt.Errorf("unsupported model parameter %q", name)
		}
		switch name {
		case "temperature":
			number, ok := numeric(value)
			if !ok || number < 0 || number > 2 {
				return errors.New("temperature must be numeric between 0 and 2")
			}
		case "max_output_tokens", "context_window":
			number, ok := numeric(value)
			if !ok || number < 1 || number != float64(int64(number)) {
				return fmt.Errorf("%s must be a positive integer", name)
			}
		case "reasoning_effort":
			level, ok := value.(string)
			if !ok || !slices.Contains(model.Capabilities.ReasoningEfforts, level) {
				return fmt.Errorf("reasoning_effort %q is not declared in capabilities.reasoning_efforts", value)
			}
		}
	}
	if model.Retry.MaxAttempts < 1 || model.Retry.Backoff != "exponential" || model.Retry.InitialInterval.Duration <= 0 || model.Retry.MaxInterval.Duration < model.Retry.InitialInterval.Duration || model.Timeout.Duration <= 0 || model.RateLimit.RequestsPerMinute < 1 {
		return errors.New("invalid retry, timeout, or rate limit configuration")
	}
	return nil
}

func validateRuntime(runtime *RuntimeConfig, base string) error {
	if !runtime.Autonomous || runtime.MaxParallelTasks < 1 || runtime.CheckpointInterval.Duration <= 0 {
		return errors.New("runtime requires autonomy, positive parallelism, and checkpoint interval")
	}
	if runtime.DataDir == "" {
		return errors.New("runtime.data_dir is required")
	}
	if !filepath.IsAbs(runtime.DataDir) {
		runtime.DataDir = filepath.Clean(filepath.Join(base, runtime.DataDir))
	}
	host, port, err := net.SplitHostPort(runtime.Listen)
	if err != nil || port == "" {
		return errors.New("runtime.listen must be a host:port address")
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("runtime.listen must be loopback until API authentication is configured")
		}
	}
	return nil
}

func toolAllowed(role, tool string) bool {
	common := map[string][]string{
		"chief-scientist": {"create_task", "delegate_task", "list_agents", "get_agent_status", "send_message", "cancel_task", "reprioritize_task", "inspect_artifact", "query_knowledge", "register_hypothesis", "request_experiment", "request_verification", "get_budget_status", "finalize_research"},
		"baby-scientist":  {"search_papers", "read_paper", "query_knowledge", "register_hypothesis", "submit_finding", "send_message"},
		"developer":       {"read_workspace", "write_workspace", "run_test", "inspect_artifact", "submit_finding", "send_message"},
		"brainstormer":    {"query_knowledge", "register_hypothesis", "submit_finding", "send_message"},
		"critic":          {"read_paper", "inspect_artifact", "query_knowledge", "request_verification", "submit_criticism", "send_message"},
	}
	return slices.Contains(common[role], tool)
}

func knownRole(role string) bool {
	return slices.Contains([]string{"chief-scientist", "baby-scientist", "developer", "brainstormer", "critic"}, role)
}

func nonNegative(value float64) bool {
	return value >= 0 && value <= 1e12 && !isNaN(value) && !isInf(value)
}

func isNaN(value float64) bool { return value != value }

func isInf(value float64) bool {
	return value > 1.7976931348623157e308 || value < -1.7976931348623157e308
}

func numeric(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	case string:
		parsed, err := strconv.ParseFloat(number, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func snapshotDigest(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		fmt.Fprintf(hash, "%s\x00%s\x00", path, files[path])
	}
	return hex.EncodeToString(hash.Sum(nil))
}
