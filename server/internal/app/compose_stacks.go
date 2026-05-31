package app

import (
	"net/http"

	"github.com/curly-hub/podorel/server/internal/agents"
	"github.com/curly-hub/podorel/server/internal/api"
	"github.com/curly-hub/podorel/server/internal/composecatalog"
	"github.com/curly-hub/podorel/server/internal/db"
)

type deployComposeStackRequest struct {
	AgentID     string `json:"agent_id"`
	StackID     string `json:"stack_id"`
	ProjectName string `json:"project_name"`
	Confirm     bool   `json:"confirm"`
}

func (a *App) handleComposeStacks(w http.ResponseWriter, r *http.Request, _ db.Session) {
	api.WriteOK(r.Context(), w, a.composeStacks)
}

func (a *App) handleDeployComposeStack(w http.ResponseWriter, r *http.Request, session db.Session) {
	if !a.requireCSRF(w, r) {
		return
	}
	var req deployComposeStackRequest
	if !decodeJSON(r, w, &req) {
		return
	}
	stack, ok := a.composeStackByID(req.StackID)
	if !ok {
		api.WriteError(r.Context(), w, http.StatusNotFound, "COMPOSE_STACK_NOT_FOUND", "Compose stack was not found.", nil)
		return
	}
	projectName := sanitizeName(req.ProjectName)
	if req.ProjectName == "" {
		projectName = sanitizeName(stack.ID)
	}
	agentID := req.AgentID
	if agentID == "" {
		agentID = db.PrimaryAgentID
	}
	if !a.canAccessAgent(session, agentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot deploy compose stacks for this agent.", nil)
		return
	}
	preview := composecatalog.PreviewCommand(stack, projectName)
	if !req.Confirm {
		api.WriteOK(r.Context(), w, map[string]any{
			"preview_command": preview,
			"project_name":    projectName,
			"stack":           stack,
		})
		return
	}
	files, err := composecatalog.BundleFiles(stack)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if err := a.proxyDeployComposeStack(r, agentID, agents.DeployComposeRequest{
		ProjectName:  projectName,
		StackID:      stack.ID,
		ComposeFiles: append([]string(nil), stack.ComposeFiles...),
		Files:        composeBundleFiles(files),
	}); err != nil {
		a.writeAgentOperationFailure(w, r, session, "compose.deploy_stack", "compose_stack", stack.ID, agentID, "deploy", err)
		return
	}
	refreshStarted := a.now()
	if err := a.refreshAgentSnapshots(r.Context(), agentID); err != nil {
		a.logger.Error(r.Context(), "agent_refresh", "could not refresh pods after compose deploy", map[string]any{"agent_id": agentID, "compose_stack_id": stack.ID, "error": err.Error()})
	}
	a.audit(r, session.UserID, "compose.deploy_stack", "compose_stack", stack.ID, "success", map[string]any{
		"agent_id":           agentID,
		"project_name":       projectName,
		"compose_file_count": len(stack.ComposeFiles),
		"bundle_file_count":  len(files),
		"refresh_started_at": refreshStarted,
	})
	api.WriteOK(r.Context(), w, map[string]any{
		"stack_id":        stack.ID,
		"project_name":    projectName,
		"preview_command": preview,
		"bundle_files":    len(files),
	})
}

func (a *App) composeStackByID(id string) (composecatalog.Stack, bool) {
	for _, stack := range a.composeStacks {
		if stack.ID == id {
			return stack, true
		}
	}
	return composecatalog.Stack{}, false
}

func composeBundleFiles(files []composecatalog.BundleFile) []agents.ComposeBundleFile {
	out := make([]agents.ComposeBundleFile, 0, len(files))
	for _, file := range files {
		out = append(out, agents.ComposeBundleFile{Path: file.Path, Content: file.Content})
	}
	return out
}
