package httpapi

import (
	"net/http"

	"drama-agent/internal/domain/billing"
	d "drama-agent/internal/domain/project"
)

func respond(w http.ResponseWriter, p d.Project, err error, status int) {
	if err != nil {
		fail(w, err)
		return
	}
	write(w, status, view(p))
}

type AssetView struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Aliases     []string        `json:"aliases"`
	Description string          `json:"description"`
	SourceIDs   []string        `json:"sourceIds"`
	Origin      string          `json:"origin"`
	Character   *d.Character    `json:"character"`
	Looks       []LookView      `json:"looks"`
	Image       *AssetImageView `json:"image"`
}

func assetView(projectID string, a d.Asset) AssetView {
	origin := a.Origin
	if origin == "" {
		origin = "model"
	}
	aliases := append([]string{}, a.Aliases...)
	source := append([]string{}, a.SourceIDs...)
	looks := make([]LookView, 0, len(a.Looks))
	for _, look := range a.Looks {
		origin := look.Origin
		if origin == "" {
			origin = "model"
		}
		looks = append(looks, LookView{ID: look.ID, Name: look.Name, Description: look.Description, Origin: origin, Image: projectAssetImage(projectID, look.ImageFileID)})
	}
	character := a.Character
	if a.Kind == "character" && character == nil {
		character = &d.Character{}
	}
	if a.Kind != "character" {
		character = nil
		looks = []LookView{}
	}
	return AssetView{ID: a.ID, Kind: a.Kind, Name: a.Name, Aliases: aliases, Description: a.Description, SourceIDs: source, Origin: origin, Character: character, Looks: looks, Image: projectAssetImage(projectID, a.ImageFileID)}
}

type VisualPromptView struct {
	Target        d.VisualTarget `json:"target"`
	Text          string         `json:"text"`
	SourceVersion uint64         `json:"sourceVersion"`
	d.ActionRef
	Stale  bool   `json:"stale"`
	Origin string `json:"origin"`
}

func runView(r d.Run) RunView {
	out := RunView{StoryboardScope: r.StoryboardScope, ID: r.ID, Status: r.Status, Stage: r.Stage, Cursor: r.Cursor, CallCount: len(r.Calls), ErrorCode: r.ErrorCode, Error: r.Error, Usage: billing.Usage{Known: true}, Contract: r.Contract, SourceVersion: r.SourceVersion, InputRevision: r.InputRevision, Model: r.Model, Target: r.Target}
	if r.Model != "" {
		budget := r.BudgetMicroCNY
		out.BudgetMicroCNY = &budget
	}
	if r.ExecutionContract() == d.VisualContract {
		ts := append([]d.VisualTarget{}, r.Targets...)
		out.Targets = &ts
	}
	for _, c := range r.Calls {
		out.Usage.Input += c.Usage.Input
		out.Usage.Output += c.Usage.Output
		out.Usage.CacheHit += c.Usage.CacheHit
		out.Usage.Known = out.Usage.Known && c.Usage.Known
	}
	return out
}

type RunView struct {
	StoryboardScope *d.StoryboardScope `json:"storyboardScope,omitempty"`
	Contract        string             `json:"contract"`
	SourceVersion   uint64             `json:"sourceVersion"`
	InputRevision   uint64             `json:"inputRevision,omitempty"`
	Model           string             `json:"model,omitempty"`
	BudgetMicroCNY  *int64             `json:"budgetMicroCny,omitempty"`
	Targets         *[]d.VisualTarget  `json:"targets,omitempty"`
	Target          *d.VisualTarget    `json:"target,omitempty"`
	ID              string             `json:"id"`
	Status          d.RunStatus        `json:"status"`
	Stage           string             `json:"stage"`
	Cursor          int                `json:"cursor"`
	CallCount       int                `json:"callCount"`
	Usage           billing.Usage      `json:"usage"`
	ErrorCode       string             `json:"errorCode,omitempty"`
	Error           string             `json:"error,omitempty"`
}

func view(p d.Project) map[string]any {
	var run *RunView
	runs := []RunView{}
	for _, r := range p.Runs {
		runs = append(runs, runView(r))
	}
	for i := len(p.Runs) - 1; i >= 0; i-- {
		r := p.Runs[i]
		if (r.Contract == d.CatalogExtractionContract || r.Contract == d.OutlineContract || r.Contract == d.VisualContract) && r.SourceVersion == p.ScriptSource.Version {
			v := runView(r)
			run = &v
			break
		}
	}
	assets := []AssetView{}
	for _, a := range p.Assets {
		assets = append(assets, assetView(p.ID, a))
	}
	prompts := []VisualPromptView{}
	for _, v := range p.VisualPrompts {
		prompts = append(prompts, VisualPromptView{Target: v.Target, Text: v.Text, SourceVersion: v.SourceVersion, ActionRef: v.ActionRef, Stale: v.Stale, Origin: v.Origin})
	}
	return map[string]any{"id": p.ID, "spaceId": p.SpaceID, "ownerId": p.OwnerID, "creatorId": p.CreatorID, "title": p.Title, "uploadedAt": p.UploadedAt, "revision": p.Revision, "source": map[string]any{"filename": p.ScriptSource.Filename, "version": p.ScriptSource.Version, "sha256": p.ScriptSource.SHA256, "spanCount": len(p.ScriptSource.Spans)}, "assets": assets, "run": run, "runs": runs, "outline": p.Outline, "settings": p.ProjectSettings, "style": p.Style, "visualPrompts": prompts, "catalogEditedByUser": p.CatalogEditedByUser, "usedOrReservedMicroCny": p.UsedMicroCNY(), "limitMicroCny": p.LimitMicroCNY}
}
