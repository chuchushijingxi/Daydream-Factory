package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"drama-agent/internal/adapter/appconfig"
	"drama-agent/internal/adapter/localembedding"
	"drama-agent/internal/application/ports"
	"drama-agent/internal/domain/fault"
)

func TestConfiguredEvidenceMissingResourcesDoNotFailServerAssembly(t *testing.T) {
	for _, scenario := range []string{"python", "weights", "endpoint"} {
		t.Run(scenario, func(t *testing.T) {
			folder := t.TempDir()
			python := filepath.Join(folder, "python")
			if scenario != "python" {
				if err := os.WriteFile(python, []byte("fictional; never executed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "endpoint" {
				for _, name := range localembedding.ModelFiles {
					if err := os.WriteFile(filepath.Join(folder, name), []byte("fictional; never loaded"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			index := configuredEvidence(appconfig.Evidence{Python: python, ModelDir: folder, Endpoint: "invalid-endpoint", Collection: "test_evidence"})
			if index == nil {
				t.Fatal("missing resources bypassed the required visual index")
			}
			var unavailable *fault.Error
			err := index.Ensure(context.Background(), ports.EvidenceDocument{})
			if !errors.As(err, &unavailable) || unavailable.Code != "EVIDENCE_UNAVAILABLE" {
				t.Fatal("visual use did not report unavailable resources", err)
			}
			want := map[string]string{"python": "Python is missing", "weights": "model.onnx is missing", "endpoint": "Milvus endpoint"}[scenario]
			if !strings.Contains(err.Error(), want) {
				t.Fatal("wrong initialization failure", err)
			}
		})
	}
}
