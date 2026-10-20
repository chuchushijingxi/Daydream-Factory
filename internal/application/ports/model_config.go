package ports

import (
	"context"

	md "drama-agent/internal/domain/modelconfig"
)

type ModelLogoStore interface {
	SaveModelLogo(context.Context, string, []byte) error
	ModelLogo(context.Context, string) ([]byte, error)
}

type RemoteModel struct {
	ModelID string `json:"modelId"`
}

type ModelDirectory func(context.Context, string, string, string) ([]RemoteModel, error)

type CredentialCipher interface {
	Seal(string) ([]byte, error)
	Open([]byte) (string, error)
}

type ModelFactory func(context.Context, string, string, string, md.ConfiguredModel) (Model, error)
