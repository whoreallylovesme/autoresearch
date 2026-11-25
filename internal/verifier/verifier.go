package verifier

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/executor"
)

type Request struct {
	ExperimentID string
	Workspace    string
	Job          executor.Job
	Artifacts    []artifact.Record
}

type Verification struct {
	ID           string             `json:"id"`
	ExperimentID string             `json:"experiment_id"`
	Type         string             `json:"type"`
	Passed       bool               `json:"passed"`
	Command      []string           `json:"command,omitempty"`
	ExitCode     int                `json:"exit_code"`
	Metrics      map[string]float64 `json:"metrics,omitempty"`
	Details      string             `json:"details"`
	ArtifactIDs  []string           `json:"artifact_ids"`
	CreatedAt    time.Time          `json:"created_at"`
}

type Verifier interface {
	Verify(context.Context, Request) (Verification, error)
}

type Benchmark struct {
	Metric    string
	Direction string
	Threshold float64
}

func (b Benchmark) Verify(ctx context.Context, request Request) (Verification, error) {
	result := Verification{ID: verificationID(), ExperimentID: request.ExperimentID, Type: "benchmark", Metrics: map[string]float64{}, ArtifactIDs: artifactIDs(request.Artifacts), CreatedAt: time.Now().UTC()}
	if b.Metric == "" || (b.Direction != "min" && b.Direction != "max") || math.IsNaN(b.Threshold) || math.IsInf(b.Threshold, 0) {
		return result, errors.New("benchmark requires a metric, finite threshold, and min/max direction")
	}
	content, err := os.ReadFile(filepath.Join(request.Workspace, "output", "metrics.json"))
	if err != nil {
		return result, err
	}
	var metrics map[string]float64
	if err := json.Unmarshal(content, &metrics); err != nil {
		return result, err
	}
	value, ok := metrics[b.Metric]
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return result, fmt.Errorf("benchmark metric %q is missing or non-finite", b.Metric)
	}
	result.Metrics = metrics
	result.Passed = (b.Direction == "min" && value >= b.Threshold) || (b.Direction == "max" && value <= b.Threshold)
	result.Details = fmt.Sprintf("%s=%g, %s threshold %g", b.Metric, value, b.Direction, b.Threshold)
	if !result.Passed {
		result.ExitCode = 1
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

type CustomCommand struct {
	Executor executor.Executor
	Command  []string
	Timeout  time.Duration
}

func (c CustomCommand) Verify(ctx context.Context, request Request) (Verification, error) {
	result := Verification{ID: verificationID(), ExperimentID: request.ExperimentID, Type: "custom-command", Command: append([]string(nil), c.Command...), ArtifactIDs: artifactIDs(request.Artifacts), CreatedAt: time.Now().UTC()}
	if c.Executor == nil || len(c.Command) == 0 {
		return result, errors.New("custom command verifier requires an executor and command")
	}
	job := request.Job
	job.ID = request.ExperimentID + "-verification"
	job.Command = append([]string(nil), c.Command...)
	if c.Timeout > 0 {
		job.Timeout = c.Timeout
	}
	if err := c.Executor.Submit(ctx, job); err != nil {
		return result, err
	}
	execution, err := c.Executor.Wait(ctx, job.ID)
	if err != nil {
		return result, err
	}
	result.ExitCode = execution.ExitCode
	result.Passed = execution.State == executor.StateCompleted && execution.ExitCode == 0
	result.Details = execution.Error
	artifacts, err := c.Executor.Collect(ctx, job.ID)
	if err == nil {
		for _, record := range artifacts {
			result.ArtifactIDs = append(result.ArtifactIDs, record.ID)
		}
	}
	return result, nil
}

func artifactIDs(records []artifact.Record) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}

func verificationID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}
