package project

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"drama-agent/internal/adapter/scriptimport"
	"drama-agent/internal/application/modelconfig"
	"drama-agent/internal/application/ports"
	uploadapp "drama-agent/internal/application/upload"
	"drama-agent/internal/domain/fault"
	"drama-agent/internal/domain/identity"
	md "drama-agent/internal/domain/modelconfig"
	d "drama-agent/internal/domain/project"
	ud "drama-agent/internal/domain/upload"
	"drama-agent/internal/testsupport/uploadfiles"
)

type cancellingFileReader struct {
	ports.FileReader
	cancel context.CancelFunc
}

func (r cancellingFileReader) ReadOriginal(ctx context.Context, id string) (ud.File, []byte, error) {
	f, raw, err := r.FileReader.ReadOriginal(ctx, id)
	r.cancel()
	return f, raw, err
}

func TestUploadedSourceConcurrencyIsolationAndCancellation(t *testing.T) {
	ctx := context.Background()
	files := uploadfiles.New()
	store := newMemory()
	s := &Service{Store: store, Files: files, Importer: scriptimport.Importer{}}
	upload := func(text string) string {
		t.Helper()
		f, err := files.Upload(ctx, uploadapp.Input{Filename: "same.txt", Data: []byte(text)})
		if err != nil {
			t.Fatal(err)
		}
		return f.ID
	}
	first := upload("完整初始虚构剧本。")
	p, err := s.CreateFromFile(ctx, "project one", first, 0, CreationChoices{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateFromFile(ctx, "project two", first, 0, CreationChoices{})
	if err != nil {
		t.Fatal(err)
	}
	inputs := []string{upload("完整虚构版本甲。"), upload("完整虚构版本乙。")}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range inputs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_, err := s.UploadFromFile(ctx, p.ID, p.Revision, id)
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, fault.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("CAS did not select exactly one source", success, conflicts)
	}
	saved, _ := store.Snapshot(ctx, p.ID)
	unselected, _ := store.Snapshot(ctx, other.ID)
	if saved.Revision != p.Revision+1 || saved.ScriptSource.Version != 2 || !reflect.DeepEqual(unselected, other) {
		t.Fatal("source change lost atomicity/project isolation")
	}
	for _, id := range inputs {
		if _, _, err := files.ReadOriginal(ctx, id); err != nil {
			t.Fatal("concurrent loser upload deleted", err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	s.Files = cancellingFileReader{FileReader: files, cancel: cancel}
	_, err = s.UploadFromFile(cancelled, p.ID, saved.Revision, first)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("late cancellation ignored", err)
	}
	after, _ := store.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(after, saved) {
		t.Fatal("cancelled replacement adopted any state")
	}
	cancelled, cancel = context.WithCancel(ctx)
	s.Files = cancellingFileReader{FileReader: files, cancel: cancel}
	_, err = s.CreateFromFile(cancelled, "cancelled creation", first, 0, CreationChoices{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled creation accepted", err)
	}
	rows, _ := store.List(ctx)
	if len(rows) != 2 {
		t.Fatal("cancelled creation left project")
	}
}

type uploadedSourceFixture struct {
	id, filename string
	raw          []byte
	reads        int
}

func (f *uploadedSourceFixture) File(_ context.Context, id string) (ud.File, error) {
	if id != f.id {
		return ud.File{}, fault.ErrNotFound
	}
	return ud.File{ID: id, Filename: f.filename}, nil
}

func (f *uploadedSourceFixture) ReadOriginal(_ context.Context, id string) (ud.File, []byte, error) {
	f.reads++
	if id != f.id {
		return ud.File{}, nil, fault.ErrNotFound
	}
	return ud.File{ID: id, Filename: f.filename}, append([]byte(nil), f.raw...), nil
}

func (f *uploadedSourceFixture) ContainerFile(ctx context.Context, space, id string) (ud.File, error) {
	return f.File(ctx, id)
}
func (f *uploadedSourceFixture) ReadContainerFile(ctx context.Context, space, id string, thumbnail bool) (ud.File, []byte, error) {
	return f.ReadOriginal(ctx, id)
}

type originalCommitFailure struct {
	ports.ProjectStore
	adopted, uncertain bool
}

func (s originalCommitFailure) Create(ctx context.Context, p d.Project) error {
	if s.adopted {
		if err := s.ProjectStore.Create(ctx, p); err != nil {
			return err
		}
	}
	return errors.New("injected commit response failure")
}
func (s originalCommitFailure) Commit(context.Context, d.Project, uint64) error {
	return fault.ErrConflict
}
func (s originalCommitFailure) Snapshot(ctx context.Context, id string) (d.Project, error) {
	if s.uncertain {
		return d.Project{}, errors.New("read unavailable")
	}
	return s.ProjectStore.Snapshot(ctx, id)
}

func TestUploadedScriptReferenceAtomicityAndChecksumWithoutModels(t *testing.T) {
	ctx := context.Background()
	raw := []byte("完整虚构剧本\r\n保留全部尾部。  \n")
	files := &uploadedSourceFixture{id: identity.New("file"), filename: "fiction.txt", raw: raw}
	for _, mode := range []struct {
		name               string
		adopted, uncertain bool
	}{{"rejected", false, false}, {"adopted", true, false}, {"unknown", false, true}} {
		t.Run(mode.name, func(t *testing.T) {
			memory := newMemory()
			s := &Service{Store: originalCommitFailure{ProjectStore: memory, adopted: mode.adopted, uncertain: mode.uncertain}, Importer: scriptimport.Importer{}, Files: files}
			p, err := s.CreateFromFile(ctx, "fiction", files.id, 0, CreationChoices{})
			if err == nil || p.SourceFileID != files.id || len(p.Original) != 0 || p.OriginalFile != nil {
				t.Fatal("file adoption/uncertainty wrong", err)
			}
			if mode.adopted {
				_, read, err := s.OriginalScript(ctx, p.ID)
				if err != nil || !reflect.DeepEqual(read, raw) {
					t.Fatal("committed source lost", err)
				}
			}
			if !reflect.DeepEqual(files.raw, raw) {
				t.Fatal("project failure changed shared upload")
			}
		})
	}
	memory := newMemory()
	s := &Service{Store: memory, Importer: scriptimport.Importer{}, Files: files}
	p, err := s.CreateFromFile(ctx, "fiction", files.id, 0, CreationChoices{})
	if err != nil {
		t.Fatal(err)
	}
	before := d.Clone(p)
	files.raw = []byte("Complete fictional replacement")
	s.Store = originalCommitFailure{ProjectStore: memory}
	_, err = s.UploadFromFile(ctx, p.ID, p.Revision, files.id)
	if !errors.Is(err, fault.ErrConflict) {
		t.Fatal("conflict lost", err)
	}
	saved, _ := memory.Snapshot(ctx, p.ID)
	if !reflect.DeepEqual(saved, before) {
		t.Fatal("conflict partly adopted source")
	}
	reads := files.reads
	_, err = s.UploadFromFile(ctx, p.ID, p.Revision-1, files.id)
	if !errors.Is(err, fault.ErrConflict) || files.reads != reads {
		t.Fatal("stale source read an upload", err)
	}
	_, _, err = s.OriginalScript(ctx, p.ID)
	expectCode(t, err, "SCRIPT_FILE_CORRUPT")
	files.raw = raw
	s.Store = memory
	p, err = s.UploadFromFile(ctx, p.ID, p.Revision, files.id)
	if err != nil || p.ScriptSource.Version != 2 || p.SourceFileID != files.id || !p.UploadedAt.Equal(*before.UploadedAt) {
		t.Fatal("replacement changed upload time or ref", err)
	}
	_, err = s.CreateFromFile(ctx, "missing", identity.New("file"), 0, CreationChoices{})
	if !errors.Is(err, fault.ErrNotFound) {
		t.Fatal("missing upload accepted", err)
	}
}

func TestUserSelectedModelAndManualExecutionWithoutModels(t *testing.T) {
	ctx := context.Background()
	projects, config := newMemory(), newConfigMemory()
	conn := &config.cfg.Connections[0]
	conn.Secret = []byte("cipher:fictional")
	for i := range conn.Models {
		conn.Models[i].Enabled = true
	}
	selected := conn.Models[1]
	var resolved []string
	provider := &modelconfig.Service{Store: config, Projects: projects, Cipher: fixtureCipher{}}
	provider.Factory = func(_ context.Context, key, endpoint, protocol string, m md.ConfiguredModel) (ports.Model, error) {
		if key != "fictional" || endpoint != conn.BaseURL || protocol != conn.Protocol || m.ModelID != selected.ModelID {
			t.Fatal("selection resolved a different provider/model")
		}
		resolved = append(resolved, m.ID)
		return preferenceOnlyModel{}, nil
	}
	if _, err := provider.Resolve(ctx, ""); err == nil {
		t.Fatal("blank choice silently selected a model")
	}
	s := &Service{Store: projects, Models: provider, Importer: scriptimport.Importer{}, Agent: unitAgent{}, Authorize: UserInitiatedExecution}
	settings := d.ProjectSettings{LLMModel: selected.ID}
	p, err := s.CreateWithChoices(ctx, "fictional", "fiction.txt", []byte("Complete fictional source."), 1, CreationChoices{Settings: &settings})
	if err != nil {
		t.Fatal(err)
	}
	settings.AspectRatio = "16:9"
	p, err = s.SaveSettings(ctx, p.ID, p.Revision, settings)
	if err != nil {
		t.Fatal(err)
	}
	changed := settings
	changed.LLMModel = conn.Models[0].ID
	_, err = s.SaveSettings(ctx, p.ID, p.Revision, changed)
	expectCode(t, err, "SETTINGS_LOCKED")
	p.Assets = []d.Asset{{ID: "c", Kind: "character", Name: "虚构人物"}}
	p.Runs = []d.Run{{ID: "p3", Contract: d.CatalogExtractionContract, SourceVersion: 1, Status: "completed", Calls: []d.Call{{ID: "old-unknown", ReservedMicroCNY: 789}}}}
	p, err = s.commit(ctx, p, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	started, err := s.StartSingleVisual(ctx, p.ID, p.Revision, d.VisualTarget{AssetID: "c"})
	if err != nil || started.LatestRun().Model != selected.ID || started.LatestRun().BudgetMicroCNY != 0 || len(started.LatestRun().Calls) != 0 || started.UsedMicroCNY() != 789 {
		t.Fatal("manual start inherited old permit/limit or changed historical cost", err)
	}
	for _, id := range resolved {
		if id != selected.ID {
			t.Fatal("default model used")
		}
	}
	conn.Models[1].Enabled = false
	if _, err = provider.Resolve(ctx, selected.ID); !errors.Is(err, fault.ErrModelUnavailable) {
		t.Fatal("disabled choice silently fell back", err)
	}
}
