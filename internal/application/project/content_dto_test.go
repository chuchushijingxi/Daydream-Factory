package project

import (
	"encoding/json"
	"testing"

	"drama-agent/internal/application/validation"
	d "drama-agent/internal/domain/project"
)

func TestStrictContentDTORequiredTypesAndReadOnlyKeys(t *testing.T) {
	valid := `{"prompts":[{"target":{"assetId":"a","lookId":null},"text":"完整\n正文","designNotes":""}]}`
	var v SaveVisualPrompts
	if err := validation.DecodeStrict([]byte(valid), &v); err != nil || v.Prompts[0].Target.LookID != "" {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"prompts":[{"target":{"assetId":"a","lookId":""},"text":"x","designNotes":""}]}`, `{"prompts":[{"target":{"assetId":"a"},"text":null,"designNotes":""}]}`, `{"prompts":[{"target":{"assetId":"a"},"text":"x"}]}`, `{"prompts":[{"target":{"assetId":"a"},"text":"x","designNotes":"","stale":false}]}`, valid + `{}`} {
		if validation.DecodeStrict([]byte(raw), &v) == nil {
			t.Fatal("invalid DTO accepted", raw)
		}
	}
	for _, raw := range []string{`{"outline":{"protagonist":"","genre":"","summary":"","background":""}}`, `{"outline":{"protagonist":"","genre":"","summary":"","background":"","logline":null}}`} {
		var o SaveOutline
		if validation.DecodeStrict([]byte(raw), &o) == nil {
			t.Fatal("missing story field")
		}
	}
	b, _ := json.Marshal(d.AssetInput{Kind: "character", Name: "x", Aliases: []string{}, Character: &d.Character{}})
	var a d.AssetInput
	if validation.DecodeStrict(b, &a) != nil {
		t.Fatal("valid asset refused")
	}
}
