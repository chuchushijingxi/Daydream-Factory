package modelconfig

import (
	"drama-agent/internal/domain/billing"
)

// Official Ark catalog verified on 2026-10-14. Output categories describe what
// is generated; multimodal understanding does not turn a language model into
// an image/video generator. Media execution is deliberately not yet exposed.
const VolcengineBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

const VolcengineProtocol = "volcengine-ark"

const DoubaoEvolvingModelID = "doubao-seed-evolving"

const DoubaoProModelID = "doubao-seed-2-1-pro-260915"

const DoubaoTurboModelID = "doubao-seed-2-1-turbo-260628"

const DoubaoLiteModelID = "doubao-seed-2-1-lite-260915"

const SeedreamProModelID = "doubao-seedream-5-0-pro-260628"

const SeedreamFlashModelID = "doubao-seedream-5-0-flash-260915"

const SeedreamLiteModelID = "doubao-seedream-5-0-260128"

const Seedance25ModelID = "doubao-seedance-2-5-260628"

const Seedance20ModelID = "doubao-seedance-2-0-260128"

const SeedanceFastModelID = "doubao-seedance-2-0-fast-260128"

const SeedanceMiniModelID = "doubao-seedance-2-0-mini-260615"

func IsVolcengineModel(id string) bool { return volcengineModelName(id) != "" }

func volcengineModelName(id string) string {
	return map[string]string{
		DoubaoEvolvingModelID: "Doubao Seed Evolving",
		DoubaoProModelID:      "Doubao Seed 2.1 Pro",
		DoubaoTurboModelID:    "Doubao Seed 2.1 Turbo",
		DoubaoLiteModelID:     "Doubao Seed 2.1 Lite",
		SeedreamProModelID:    "Doubao Seedream 5.0 Pro",
		SeedreamFlashModelID:  "Doubao Seedream 5.0 Flash",
		SeedreamLiteModelID:   "Doubao Seedream 5.0 Lite",
		Seedance25ModelID:     "Doubao Seedance 2.5",
		Seedance20ModelID:     "Doubao Seedance 2.0",
		SeedanceFastModelID:   "Doubao Seedance 2.0 Fast",
		SeedanceMiniModelID:   "Doubao Seedance 2.0 Mini",
	}[id]
}

func volcenginePreset() ModelConnection {
	text := func(id string, window int, input, output, cache float64) ConfiguredModel {
		return ConfiguredModel{ID: id, ModelID: id, Category: "analysis", ContextWindow: window, MaxOutputTokens: 256000, Pricing: &billing.TokenPricing{Input: input, Output: output, CacheHitHundredths: cache * 100}}
	}
	media := func(id, category string) ConfiguredModel {
		return ConfiguredModel{ID: id, ModelID: id, Category: category}
	}
	return ModelConnection{ID: "volcengine", Name: "火山引擎", Kind: "official", Protocol: VolcengineProtocol, BaseURL: VolcengineBaseURL, Models: []ConfiguredModel{
		text(DoubaoEvolvingModelID, 1024000, 6, 30, 1.2),
		text(DoubaoProModelID, 1024000, 6, 30, 1.2),
		text(DoubaoTurboModelID, 256000, 3, 15, .6),
		text(DoubaoLiteModelID, 1024000, .8, 2.7, .16),
		media(SeedreamProModelID, "image"), media(SeedreamFlashModelID, "image"),
		media(Seedance25ModelID, "video"), media(Seedance20ModelID, "video"),
		media(SeedanceFastModelID, "video"), media(SeedanceMiniModelID, "video"),
		media(SeedreamLiteModelID, "image"),
	}}
}
