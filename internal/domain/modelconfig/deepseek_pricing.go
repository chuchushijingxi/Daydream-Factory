package modelconfig

import (
	"drama-agent/internal/domain/billing"
)

func PricingForModel(id string) billing.TokenPricing {
	if id == DeepSeekProModelID {
		return billing.TokenPricing{Input: 9, Output: 27, CacheHitHundredths: 30}
	}
	return billing.TokenPricing{Input: 2, Output: 8, CacheHitHundredths: 4}
}
