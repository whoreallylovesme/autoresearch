package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := newRootCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "researchctl:", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "researchctl", Short: "Operate an AutoResearch runtime", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(newInitCommand(), newValidateCommand(), newRunCommand())
	for _, operation := range []struct {
		name string
		path string
	}{
		{"status", "/api/v1/run/status"},
		{"agents", "/api/v1/agents"},
		{"tasks", "/api/v1/tasks"},
		{"budget", "/api/v1/budget"},
		{"report", "/api/v1/report"},
	} {
		root.AddCommand(newGetCommand(operation.name, operation.path))
	}
	for _, action := range []string{"stop", "pause", "resume"} {
		root.AddCommand(newControlCommand(action))
	}
	root.AddCommand(newFinalizeCommand(), newLogsCommand(), newChatCommand())
	return root
}

func newInitCommand() *cobra.Command {
	var directory string
	command := &cobra.Command{Use: "init", Short: "Create a standalone mock research project", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := config.Scaffold(directory); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "initialized AutoResearch project in %s\n", directory)
		return nil
	}}
	command.Flags().StringVar(&directory, "directory", "autoresearch-project", "project directory")
	return command
}

func newValidateCommand() *cobra.Command {
	var configPath string
	command := &cobra.Command{Use: "validate", Short: "Validate configuration and references", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		configuration, err := config.Load(configPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "configuration valid: %s\nsnapshot: %s\n", configuration.Name, configuration.Snapshot.Digest)
		return nil
	}}
	command.Flags().StringVar(&configPath, "config", "configs/autoresearch.yaml", "main configuration path")
	return command
}

func newRunCommand() *cobra.Command {
	var configPath, apiURL string
	var timeout time.Duration
	command := &cobra.Command{Use: "run", Short: "Start research through the runtime API and wait for its report", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		configuration, err := config.Load(configPath)
		if err != nil {
			return err
		}
		if apiURL == "" {
			apiURL = "http://" + configuration.Runtime.Listen
		}
		if err := call(cmd.Context(), http.MethodPost, apiURL+"/api/v1/run/start", nil, nil); err != nil {
			return fmt.Errorf("start research (is researchd running?): %w", err)
		}
		runContext, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		return waitForReport(runContext, apiURL)
	}}
	command.Flags().StringVar(&configPath, "config", "configs/autoresearch.yaml", "main configuration path")
	command.Flags().StringVar(&apiURL, "api", "", "runtime API base URL")
	command.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "maximum time to wait for research")
	return command
}

func newGetCommand(name, path string) *cobra.Command {
	var apiURL string
	command := &cobra.Command{Use: name, Short: "Read " + name + " through the runtime API", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		baseURL := apiURL
		if baseURL == "" {
			baseURL = "http://127.0.0.1:8080"
		}
		var result any
		if err := call(cmd.Context(), http.MethodGet, baseURL+path, nil, &result); err != nil {
			return err
		}
		if name == "report" {
			fmt.Fprintln(cmd.OutOrStdout(), result.(map[string]any)["report"])
			return nil
		}
		return printJSON(result)
	}}
	command.Flags().StringVar(&apiURL, "api", "", "runtime API base URL")
	return command
}

func newControlCommand(action string) *cobra.Command {
	var apiURL string
	command := &cobra.Command{Use: action, Short: "Send " + action + " to the runtime", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		baseURL := apiURL
		if baseURL == "" {
			baseURL = "http://127.0.0.1:8080"
		}
		return call(cmd.Context(), http.MethodPost, baseURL+"/api/v1/run/"+action, nil, nil)
	}}
	command.Flags().StringVar(&apiURL, "api", "", "runtime API base URL")
	return command
}

func newFinalizeCommand() *cobra.Command {
	var apiURL string
	command := &cobra.Command{Use: "finalize", Short: "Request research finalization", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		baseURL := apiURL
		if baseURL == "" {
			baseURL = "http://127.0.0.1:8080"
		}
		var result any
		if err := call(cmd.Context(), http.MethodPost, baseURL+"/api/v1/run/finalize", nil, &result); err != nil {
			return err
		}
		return printJSON(result)
	}}
	command.Flags().StringVar(&apiURL, "api", "", "runtime API base URL")
	return command
}

func newLogsCommand() *cobra.Command {
	var apiURL string
	var follow bool
	command := &cobra.Command{Use: "logs", Short: "Read research events", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		baseURL := apiURL
		if baseURL == "" {
			baseURL = "http://127.0.0.1:8080"
		}
		if follow {
			return streamEvents(cmd.Context(), baseURL)
		}
		var result any
		if err := call(cmd.Context(), http.MethodGet, baseURL+"/api/v1/events", nil, &result); err != nil {
			return err
		}
		return printJSON(result)
	}}
	command.Flags().StringVar(&apiURL, "api", "", "runtime API base URL")
	command.Flags().BoolVar(&follow, "follow", false, "follow the event stream")
	return command
}

func newChatCommand() *cobra.Command {
	var apiURL, message string
	var directive bool
	command := &cobra.Command{Use: "chat", Short: "Send a persistent operator message to the Chief Scientist", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if message == "" {
			return errors.New("chat requires --message")
		}
		baseURL := apiURL
		if baseURL == "" {
			baseURL = "http://127.0.0.1:8080"
		}
		payload, _ := json.Marshal(map[string]any{"message": message, "directive": directive})
		var result any
		if err := call(cmd.Context(), http.MethodPost, baseURL+"/api/v1/chat", payload, &result); err != nil {
			return err
		}
		return printJSON(result)
	}}
	command.Flags().StringVar(&apiURL, "api", "", "runtime API base URL")
	command.Flags().StringVar(&message, "message", "", "operator chat message")
	command.Flags().BoolVar(&directive, "directive", false, "send an authoritative operator directive")
	return command
}

func waitForReport(ctx context.Context, baseURL string) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status research.Status
		if err := call(ctx, http.MethodGet, baseURL+"/api/v1/run/status", nil, &status); err != nil {
			return err
		}
		switch status.State {
		case research.StateCompleted:
			var report map[string]string
			if err := call(ctx, http.MethodGet, baseURL+"/api/v1/report", nil, &report); err != nil {
				return err
			}
			fmt.Println(report["report"])
			return nil
		case research.StateFailed, research.StateCancelled:
			return fmt.Errorf("research ended in state %s", status.State)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func streamEvents(ctx context.Context, baseURL string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/events/stream", nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			fmt.Println(strings.TrimPrefix(scanner.Text(), "data: "))
		}
	}
	return scanner.Err()
}

func call(ctx context.Context, method, url string, body []byte, destination any) error {
	request, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	if destination != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(destination)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return nil
}

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return fmt.Errorf("API returned %s: %s", response.Status, strings.TrimSpace(string(body)))
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
