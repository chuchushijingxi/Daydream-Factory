package modelconfig

import (
	"drama-agent/internal/domain/billing"
)

// 官方 Gemini 目录已于 2026-10-14 核实。语言执行走原生适配器；
// 图片条目仍只是配置与项目偏好。
const GoogleBaseURL = "https://generativelanguage.googleapis.com/v1beta"

const GoogleProtocol = "gemini"

const GeminiArgonModelID = "gemini-4-argon"

const GeminiFlashModelID = "gemini-3.8-flash"

const GeminiLiteModelID = "gemini-3.5-flash-lite"

const NanoBananaModelID = "gemini-2.5-flash-image"

const NanoBananaProModelID = "gemini-3-pro-image"

const NanoBanana2ModelID = "gemini-3.1-flash-image"

const NanoBanana21ModelID = "gemini-nano-banana-2.1"

const NanoBananaLiteModelID = "gemini-3.1-flash-lite-image"

func IsGoogleModel(id string) bool { return googleModelName(id) != "" }

func googleModelName(id string) string {
	return map[string]string{
		GeminiArgonModelID:    "Gemini 4 Argon",
		GeminiFlashModelID:    "Gemini 3.8 Flash",
		GeminiLiteModelID:     "Gemini 3.5 Flash-Lite",
		NanoBananaModelID:     "Nano Banana",
		NanoBananaProModelID:  "Nano Banana Pro",
		NanoBanana2ModelID:    "Nano Banana 2",
		NanoBanana21ModelID:   "Nano Banana 2.1",
		NanoBananaLiteModelID: "Nano Banana 2 Lite",
	}[id]
}

func googlePreset() ModelConnection {
	conn := ModelConnection{ID: "google", Name: "Google Gemini", Kind: "official", Protocol: GoogleProtocol, BaseURL: GoogleBaseURL}
	for _, id := range []string{NanoBanana21ModelID, NanoBananaProModelID, NanoBanana2ModelID, NanoBananaLiteModelID, NanoBananaModelID} {
		conn.Models = append(conn.Models, ConfiguredModel{ID: id, ModelID: id, Category: "image"})
	}
	// 预算估算采用已公布的非促销 Standard 美元价，并按固定 7 元人民币/美元
	// 记账折算，不是实时汇率或厂商账单。
	text := func(id string, input, output, cache float64) {
		conn.Models = append(conn.Models, ConfiguredModel{ID: id, ModelID: id, Category: "analysis", ContextWindow: 1048576, MaxOutputTokens: 65536, Pricing: &billing.TokenPricing{Input: input * 7, Output: output * 7, CacheHitHundredths: cache * 700}})
	}
	// Argon 使用标准同步生成，没有长解码续传。
	text(GeminiArgonModelID, 4, 20, .2)
	text(GeminiFlashModelID, 1.5, 7.5, .15)
	text(GeminiLiteModelID, .3, 2.5, .03)
	return conn
}
