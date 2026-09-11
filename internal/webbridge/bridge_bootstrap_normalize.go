package webbridge

import (
	"strings"
)

func normalizeBootstrapState(state BootstrapState) BootstrapState {
	state.Workspaces = nonNilSlice(state.Workspaces)
	state.RemoteWorkspaces = nonNilSlice(state.RemoteWorkspaces)
	state.Worktrees = nonNilSlice(state.Worktrees)
	state.Models = nonNilSlice(state.Models)
	state.ModelCatalog.Providers = nonNilSlice(state.ModelCatalog.Providers)
	state.ModelCatalog.Presets = nonNilSlice(state.ModelCatalog.Presets)
	state.MCPServers = nonNilSlice(state.MCPServers)
	state.LSPServers = nonNilSlice(state.LSPServers)
	state.Skills = nonNilSlice(state.Skills)
	state.Plugins = nonNilSlice(state.Plugins)
	state.CostItems = nonNilSlice(state.CostItems)
	state.Versions = nonNilSlice(state.Versions)
	state.RulesState.Workspaces = nonNilSlice(state.RulesState.Workspaces)
	state.Tasks = nonNilSlice(state.Tasks)
	state.Sessions = nonNilSlice(state.Sessions)
	state.Messages = nonNilSlice(state.Messages)
	state.Prompts = nonNilSlice(state.Prompts)
	state.Notifications = nonNilSlice(state.Notifications)
	state.AutomationTemplates = nonNilSlice(state.AutomationTemplates)
	state.AutomationRuns = nonNilSlice(state.AutomationRuns)
	state.CommandPalette = nonNilSlice(state.CommandPalette)
	state.InputSuggestions = nonNilSlice(state.InputSuggestions)
	state.MigrationBoundaries = nonNilSlice(state.MigrationBoundaries)
	state.ResourceChecks = nonNilSlice(state.ResourceChecks)
	state.Memory.Documents = nonNilSlice(state.Memory.Documents)
	if strings.TrimSpace(state.UpdateInstall.Status) == "" {
		state.UpdateInstall.Status = "idle"
	}
	if strings.TrimSpace(state.ReasoningLevel) == "" {
		state.ReasoningLevel = "off"
	}
	state.ExecutionMode = normalizeExecutionMode(state.ExecutionMode)
	state.Permission.ExecutionMode = normalizeExecutionMode(state.Permission.ExecutionMode)
	state.Settings.Runtime.ExecutionMode = normalizeExecutionMode(state.Settings.Runtime.ExecutionMode)
	state.Permission.SandboxMode = NormalizeSandboxMode(state.Permission.SandboxMode)
	state.Settings.Runtime.SandboxMode = NormalizeSandboxMode(state.Settings.Runtime.SandboxMode)
	state.Permission.AllowAll = state.Permission.SandboxMode == "danger-full-access"

	state.Permission.AllowedCategories = nonNilSlice(state.Permission.AllowedCategories)
	state.Bash.Output = nonNilSlice(state.Bash.Output)
	state.Terminal.Sessions = nonNilSlice(state.Terminal.Sessions)
	state.Diagnostics.LogTail = nonNilSlice(state.Diagnostics.LogTail)
	state.Diagnostics.LSPDiagnostics = nonNilSlice(state.Diagnostics.LSPDiagnostics)
	state.Diagnostics.ContextPreview = nonNilSlice(state.Diagnostics.ContextPreview)

	for index := range state.Skills {
		state.Skills[index].AllowedTools = nonNilSlice(state.Skills[index].AllowedTools)
	}
	for index := range state.Messages {
		state.Messages[index].Attachments = nonNilSlice(state.Messages[index].Attachments)
		state.Messages[index].RuntimeEvents = nonNilSlice(state.Messages[index].RuntimeEvents)
		state.Messages[index].Prompts = nonNilSlice(state.Messages[index].Prompts)
		for promptIndex := range state.Messages[index].Prompts {
			if strings.TrimSpace(state.Messages[index].Prompts[promptIndex].Status) == "" {
				state.Messages[index].Prompts[promptIndex].Status = "pending"
			}
		}
		if state.Messages[index].ChangeSet != nil {
			state.Messages[index].ChangeSet.Files = nonNilSlice(state.Messages[index].ChangeSet.Files)
		}
		if strings.TrimSpace(state.Messages[index].UpdatedAt) == "" {
			state.Messages[index].UpdatedAt = state.Messages[index].CreatedAt
		}
		if strings.TrimSpace(state.Messages[index].RuntimeSummary) == "" {
			state.Messages[index].RuntimeSummary = runtimeSummaryForMessage(state.Messages[index])
		}
	}
	for index := range state.Prompts {
		if strings.TrimSpace(state.Prompts[index].Status) == "" {
			state.Prompts[index].Status = "pending"
		}
	}
	for index := range state.ModelCatalog.Providers {
		state.ModelCatalog.Providers[index].DefaultModels = nonNilSlice(state.ModelCatalog.Providers[index].DefaultModels)
	}
	for index := range state.ModelCatalog.Presets {
		state.ModelCatalog.Presets[index].Tags = nonNilSlice(state.ModelCatalog.Presets[index].Tags)
	}
	for index := range state.MigrationBoundaries {
		state.MigrationBoundaries[index].Targets = nonNilSlice(state.MigrationBoundaries[index].Targets)
		state.MigrationBoundaries[index].Notes = nonNilSlice(state.MigrationBoundaries[index].Notes)
	}

	return state
}
