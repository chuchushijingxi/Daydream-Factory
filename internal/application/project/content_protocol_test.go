package project

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	d "drama-agent/internal/domain/project"
)

// 固定重构前实际工具合同；领域结构变化不能顺带改变模型的可写 schema。
func TestAcceptedContentToolProtocols(t *testing.T) {
	for contract, expected := range map[string]string{
		d.OutlineContract: "2ef6f5460c9f4bf087cf7b3a3506c42e0507421f6e5bd7a444e3275769d819df",
		d.VisualContract:  "0a47dc45ca5868c8466c3bc85a5c9c54559bb6b80f253c644420496a6a018495",
	} {
		t.Run(contract, func(t *testing.T) {
			definitions := TaskTools(contract)
			// Preserve the accepted save/finish protocol exactly; the two new
			// read-only evidence tools have their own schema/actual-wire checks.
			if contract == d.VisualContract {
				definitions = definitions[:2]
			}
			raw, err := json.Marshal(definitions)
			if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != expected {
				t.Fatal("既有工具描述或 schema 发生变化", err)
			}
		})
	}
}
