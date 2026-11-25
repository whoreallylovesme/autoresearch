package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func Metrics(ctx context.Context, writer http.ResponseWriter, runtime *research.Runtime) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	status := runtime.Status()
	fmt.Fprintf(writer, "autoresearch_research_info{name=\"%s\",state=\"%s\"} 1\n", escape(status.Name), status.State)
	if !status.StartedAt.IsZero() {
		fmt.Fprintf(writer, "autoresearch_started_timestamp_seconds %.3f\n", float64(status.StartedAt.UnixNano())/1e9)
	}
	agents := map[string]int{}
	for _, agent := range runtime.Agents() {
		agents[agent.State]++
	}
	writeGauge(writer, "autoresearch_agents", "state", agents)
	tasks := map[string]int{}
	for _, task := range runtime.Tasks() {
		tasks[string(task.State)]++
	}
	writeGauge(writer, "autoresearch_tasks", "state", tasks)
	experiments := map[string]int{}
	records, _ := runtime.Experiments(ctx)
	for _, record := range records {
		experiments[string(record.Status)]++
	}
	writeGauge(writer, "autoresearch_experiments", "status", experiments)
	hypotheses := map[string]int{}
	for _, node := range runtime.Graph().Nodes {
		if node.Type == "hypothesis" {
			hypotheses[node.Status]++
		}
	}
	writeGauge(writer, "autoresearch_hypotheses", "status", hypotheses)
	fmt.Fprintf(writer, "autoresearch_events_total %d\n", len(runtime.Events()))
	fmt.Fprintf(writer, "autoresearch_budget_confirmed_usd %.6f\n", float64(status.Budget.Confirmed.CostMicros)/1e6)
	fmt.Fprintf(writer, "autoresearch_budget_reserved_usd %.6f\n", float64(status.Budget.Reserved.CostMicros)/1e6)
	fmt.Fprintf(writer, "autoresearch_budget_uncertain_usd %.6f\n", float64(status.Budget.Uncertain.CostMicros)/1e6)
	papers, _ := runtime.Papers(ctx)
	fmt.Fprintf(writer, "autoresearch_papers %d\n", len(papers))
}

func NewTraceparent() string {
	traceID := make([]byte, 16)
	spanID := make([]byte, 8)
	_, _ = rand.Read(traceID)
	_, _ = rand.Read(spanID)
	return "00-" + hex.EncodeToString(traceID) + "-" + hex.EncodeToString(spanID) + "-01"
}

func writeGauge(writer http.ResponseWriter, name, label string, values map[string]int) {
	for value, count := range values {
		fmt.Fprintf(writer, "%s{%s=\"%s\"} %d\n", name, label, escape(value), count)
	}
}

func escape(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return strings.ReplaceAll(value, "\n", "\\n")
}
