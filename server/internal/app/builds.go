package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"

	"github.com/curly-hub/podorel/server/internal/agents"
	"github.com/curly-hub/podorel/server/internal/api"
	"github.com/curly-hub/podorel/server/internal/db"
)

func (a *App) createImageBuild(ctx context.Context, agentID string, imageName string, dockerfile string, metadata map[string]any) (db.ImageBuild, error) {
	sum := sha256.Sum256([]byte(dockerfile))
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["logs"] = []any{buildLogEntry(a.now(), "info", "Build queued", nil)}
	build, err := a.store.CreateImageBuild(ctx, db.ImageBuild{
		AgentID:        agentID,
		ImageName:      imageName,
		DockerfileHash: fmt.Sprintf("sha256:%x", sum[:]),
		Status:         "queued",
		StartedAt:      a.now(),
		Metadata:       metadata,
	})
	if err != nil {
		return db.ImageBuild{}, err
	}
	go a.runImageBuild(build.ID, agentID, imageName, dockerfile)
	return build, nil
}

func (a *App) runImageBuild(buildID string, agentID string, imageName string, dockerfile string) {
	ctx := context.Background()
	build, err := a.store.ImageBuildByID(ctx, buildID)
	if err != nil {
		return
	}
	build.Status = "running"
	_ = a.store.UpdateImageBuild(ctx, build)
	_, _ = a.store.AppendImageBuildLog(ctx, buildID, buildLogEntry(a.now(), "info", "Podman build started", map[string]any{"image_name": imageName}))
	err = a.buildImageWithAgent(ctx, agentID, agents.BuildImageRequest{ImageName: imageName, Dockerfile: dockerfile})
	build, loadErr := a.store.ImageBuildByID(ctx, buildID)
	if loadErr != nil {
		return
	}
	if build.Metadata == nil {
		build.Metadata = map[string]any{}
	}
	build.FinishedAt = a.now()
	if err != nil {
		build.Status = "failed"
		build.Metadata["error"] = err.Error()
		_ = a.store.UpdateImageBuild(ctx, build)
		_, _ = a.store.AppendImageBuildLog(ctx, buildID, buildLogEntry(a.now(), "error", "Podman build failed", map[string]any{"error": err.Error()}))
		return
	}
	build.Status = "complete"
	_ = a.store.UpdateImageBuild(ctx, build)
	_, _ = a.store.AppendImageBuildLog(ctx, buildID, buildLogEntry(a.now(), "info", "Podman build completed", map[string]any{"image_name": imageName}))
}

func (a *App) buildImageWithAgent(ctx context.Context, agentID string, req agents.BuildImageRequest) error {
	_, client, ok, err := a.agentClient(ctx, agentID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("agent token unavailable for %s", agentID)
	}
	return client.BuildImage(ctx, req)
}

func buildLogEntry(ts time.Time, level string, message string, fields map[string]any) map[string]any {
	entry := map[string]any{"timestamp": ts.Format(time.RFC3339Nano), "level": level, "message": message}
	if len(fields) > 0 {
		entry["fields"] = fields
	}
	return entry
}

func (a *App) handleBuildsWebSocket(w http.ResponseWriter, r *http.Request, session db.Session) {
	buildID := r.URL.Query().Get("build_id")
	if buildID == "" {
		api.WriteError(r.Context(), w, http.StatusBadRequest, "BUILD_ID_REQUIRED", "build_id is required.", nil)
		return
	}
	build, err := a.store.ImageBuildByID(r.Context(), buildID)
	if err != nil {
		a.writeStoreError(w, r, err)
		return
	}
	if !a.canAccessAgent(session, build.AgentID) {
		api.WriteError(r.Context(), w, http.StatusForbidden, "FORBIDDEN", "Session cannot access this build.", nil)
		return
	}
	conn, err := acceptWebSocket(w, r)
	if err != nil {
		return
	}
	defer conn.Close()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastPayload := ""
	for {
		build, err := a.store.ImageBuildByID(r.Context(), buildID)
		if err != nil {
			return
		}
		payload := mustJSON(map[string]any{"type": "build", "build": build})
		if payload != lastPayload {
			_ = conn.SetWriteDeadline(a.now().Add(webSocketWriteWait))
			if err := writeWebSocketText(conn, payload); err != nil {
				return
			}
			lastPayload = payload
		}
		if build.Status == "complete" || build.Status == "failed" {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
