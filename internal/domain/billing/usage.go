package billing

type Usage struct {
	Input    int  `json:"inputTokens"`
	Output   int  `json:"outputTokens"`
	CacheHit int  `json:"cacheHitTokens"`
	Known    bool `json:"known"`
}
