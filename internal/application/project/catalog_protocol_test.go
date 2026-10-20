package project

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// 摘要锁定工具与可写 schema 的运输合同；Prompt 正文由下方全文 fixture 锁定。
// 结构重构不能改这些基准；业务调整须有明确范围并同步基准。
func TestAcceptedCatalogExtractionProtocol(t *testing.T) {
	tools, err := json.Marshal(Tools())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ name, text, digest string }{
		{"提取 schema", OutputSchema(), "de8b502ca5205dbefe1fd1e7e4b7e7e01fa2446b4c936dc7f4554a60617943f2"},
		{"工具运输协议", string(tools), "7a4d5478bb8e7089327079bfff5d6ea3a4073432bc83cff8dad7d5f6cf994dc2"},
	} {
		t.Run(item.name, func(t *testing.T) {
			if actual := fmt.Sprintf("%x", sha256.Sum256([]byte(item.text))); actual != item.digest {
				t.Fatalf("已验收协议发生变化: got %s, want %s", actual, item.digest)
			}
		})
	}
}

func TestCatalogPromptsMatchApprovedFixtures(t *testing.T) {
	for fixture, prompt := range map[string]string{"extract": ExtractPrompt, "correct": CorrectPrompt, "source-instruction": strings.TrimPrefix(modelSourceInstruction, "\n\n")} {
		raw, err := os.ReadFile("testdata/contracts/" + fixture + ".txt")
		if err != nil || string(raw) != prompt {
			t.Fatal("approved prompt differs", fixture, err)
		}
	}
}
