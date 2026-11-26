package main

import "testing"

func TestRootExposesAllOperatorCommands(t *testing.T) {
	root := newRootCommand()
	commands := []string{"init", "validate", "run", "status", "agents", "tasks", "budget", "logs", "stop", "pause", "resume", "finalize", "chat", "report"}
	for _, name := range commands {
		command, _, err := root.Find([]string{name})
		if err != nil || command == nil || command.Name() != name {
			t.Fatalf("command %q is not registered: %v", name, err)
		}
	}
	run, _, _ := root.Find([]string{"run"})
	if run.Flag("config") == nil || run.Flag("api") == nil || run.Flag("timeout") == nil {
		t.Fatal("run command is missing config/api/timeout flags")
	}
	chat, _, _ := root.Find([]string{"chat"})
	if chat.Flag("message") == nil || chat.Flag("directive") == nil {
		t.Fatal("chat command is missing message/directive flags")
	}
}
