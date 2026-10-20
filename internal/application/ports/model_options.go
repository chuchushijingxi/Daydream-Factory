package ports

import "drama-agent/internal/domain/credits"
import md "drama-agent/internal/domain/modelconfig"

type ModelOption struct {
	MediaProfile   *md.MediaProfile   `json:"mediaProfile,omitempty"`
	BillingUnit    string             `json:"billingUnit,omitempty"`
	Free           bool               `json:"free"`
	CreditPrice    *credits.PriceRule `json:"creditPrice"`
	PriceVersion   uint64             `json:"priceVersion"`
	LogoURL        string             `json:"logoUrl,omitempty"`
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Description    string             `json:"description"`
	ConnectionName string             `json:"connectionName,omitempty"`
	Provider       string             `json:"provider,omitempty"`
	ModelID        string             `json:"modelId,omitempty"`
}

type ModelOptions struct {
	Catalog  []ModelOption `json:"catalog,omitempty"`
	Analysis []ModelOption `json:"analysis"`
	Image    []ModelOption `json:"image"`
	Video    []ModelOption `json:"video"`
}
