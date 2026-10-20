package project

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
	u "drama-agent/internal/domain/upload"
)

type imageReader struct {
	ports.FileReader
	file u.File
	err  error
	read func()
}

func (f imageReader) File(context.Context, string) (u.File, error) {
	if f.read != nil {
		f.read()
	}
	return f.file, f.err
}
func imageFile() u.File {
	id := "file_" + strings.Repeat("a", 32)
	original := u.Blob{Bucket: "test", ObjectKey: u.ObjectKey(id, "original"), SizeBytes: 99, ContentType: "image/png", SHA256: strings.Repeat("a", 64)}
	thumbnail := original
	thumbnail.ObjectKey = u.ObjectKey(id, "thumbnail.webp")
	thumbnail.ContentType = "image/webp"
	return u.File{ID: id, Filename: "image.png", Original: original, Thumbnail: &u.Thumbnail{Blob: thumbnail, Width: 20, Height: 10}}
}
func TestImageAssociationPreservesTextPromptsHistoryAndSurvivesTextEdits(t *testing.T) {
	s, p := singleFixture(t)
	ctx := context.Background()
	p.VisualPrompts = []d.VisualPrompt{{VisualPromptInput: d.VisualPromptInput{Target: d.VisualTarget{AssetID: "c"}, Text: "Retained effective or stale prompt"}, SourceVersion: 1}}
	if err := s.Store.Commit(ctx, p, p.Revision); err != nil {
		t.Fatal(err)
	}
	before := d.Clone(p)
	s.Files = imageReader{file: imageFile()}
	targets := []d.VisualTarget{{AssetID: "c"}, {AssetID: "c", LookID: "l"}, {AssetID: "loc"}, {AssetID: "prop"}}
	for _, target := range targets {
		var err error
		p, err = s.SaveImage(ctx, p.ID, p.Revision, target, imageFile().ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	expected := d.Clone(before)
	expected.Revision += 4
	expected.CatalogEditedByUser = true
	for i := range expected.Assets {
		expected.Assets[i].ImageFileID = imageFile().ID
		for j := range expected.Assets[i].Looks {
			expected.Assets[i].Looks[j].ImageFileID = imageFile().ID
		}
	}
	if !reflect.DeepEqual(p, expected) {
		t.Fatal("image association changed text, source, history, fees or prompts")
	}
	p, err := s.SaveLook(ctx, p.ID, p.Revision, "c", "l", d.LookInput{Name: "New look name", Description: "new description"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Assets[0].Looks[0].ImageFileID != imageFile().ID {
		t.Fatal("look edit lost file")
	}
	p, err = s.SaveAsset(ctx, p.ID, p.Revision, "loc", d.AssetInput{Kind: "location", Name: "New location", Aliases: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Asset("loc").ImageFileID != imageFile().ID {
		t.Fatal("asset edit lost file")
	}
	other := d.Clone(before)
	other.ID = "other-project"
	if err = s.Store.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	foreign, err := s.Project(ctx, other.ID)
	if err != nil || !reflect.DeepEqual(foreign, other) {
		t.Fatal("foreign project changed", err)
	}
}
func TestImageFailuresDoNotPartiallyWrite(t *testing.T) {
	for _, name := range []string{"stale", "missing-asset", "wrong-parent", "missing-file", "non-image", "no-thumbnail", "invalid-metadata", "cancelled", "cancel-after-read", "not-ready", "running", "commit-failure"} {
		t.Run(name, func(t *testing.T) {
			s, p := singleFixture(t)
			ctx := context.Background()
			revision := p.Revision
			target := d.VisualTarget{AssetID: "c"}
			f := imageReader{file: imageFile()}
			switch name {
			case "stale":
				revision++
			case "missing-asset":
				target.AssetID = "foreign-id"
			case "wrong-parent":
				target = d.VisualTarget{AssetID: "prop", LookID: "l"}
			case "missing-file":
				f.err = fault.ErrNotFound
			case "non-image":
				f.file.Original.ContentType = "video/mp4"
			case "no-thumbnail":
				f.file.Thumbnail = nil
			case "invalid-metadata":
				f.file.Thumbnail.Blob.ObjectKey = f.file.Original.ObjectKey
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "cancel-after-read":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				f.read = cancel
			case "not-ready":
				p.Runs = nil
			case "running":
				p.Runs[0].Status = "running"
			case "commit-failure":
				s.Store = imageFailStore{s.Store}
			}
			if name == "not-ready" || name == "running" {
				if err := s.Store.Commit(context.Background(), p, p.Revision); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := s.Store.Snapshot(context.Background(), p.ID)
			s.Files = f
			if _, err := s.SaveImage(ctx, p.ID, revision, target, f.file.ID); err == nil {
				t.Fatal("invalid write accepted")
			}
			after, _ := s.Store.Snapshot(context.Background(), p.ID)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed association partially committed")
			}
		})
	}
}

type imageFailStore struct{ ports.ProjectStore }

func (imageFailStore) Commit(context.Context, d.Project, uint64) error {
	return errors.New("storage failed")
}
func TestConcurrentImageAssociationsCommitOnlyOneVersion(t *testing.T) {
	s, p := singleFixture(t)
	arrivals := make(chan struct{}, 2)
	release := make(chan struct{})
	s.Files = imageReader{file: imageFile(), read: func() { arrivals <- struct{}{}; <-release }}
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, target := range []d.VisualTarget{{AssetID: "c"}, {AssetID: "loc"}} {
		workers.Add(1)
		go func(target d.VisualTarget) {
			defer workers.Done()
			_, err := s.SaveImage(context.Background(), p.ID, p.Revision, target, imageFile().ID)
			results <- err
		}(target)
	}
	<-arrivals
	<-arrivals
	close(release)
	workers.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, fault.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	after, _ := s.Project(context.Background(), p.ID)
	if success != 1 || conflict != 1 || after.Revision != p.Revision+1 {
		t.Fatal("CAS failed")
	}
}
func TestTextModelRequestsExcludeUploadedImagesWithoutLosingFacts(t *testing.T) {
	s, p := singleFixture(t)
	p.Assets[0].ImageFileID = imageFile().ID
	p.Assets[0].Looks[0].ImageFileID = imageFile().ID
	before := d.Clone(p)
	for _, contract := range []string{d.CatalogExtractionContract, d.VisualContract} {
		p.Runs = append(p.Runs, d.Run{ID: "request-only", Contract: contract, SourceVersion: 1, Status: "running", Targets: []d.VisualTarget{{AssetID: "c"}, {AssetID: "c", LookID: "l"}}})
		request, _, err := s.request(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(request)
		if strings.Contains(string(raw), imageFile().ID) || strings.Contains(string(raw), "imageFileId") || !strings.Contains(string(raw), "完整 P3 说明") || !strings.Contains(string(raw), "黑礼服") {
			t.Fatal("image leaked or facts lost", request.Input)
		}
		p.Runs = p.Runs[:len(p.Runs)-1]
	}
	if !reflect.DeepEqual(p, before) {
		t.Fatal("request projection modified project")
	}
}
