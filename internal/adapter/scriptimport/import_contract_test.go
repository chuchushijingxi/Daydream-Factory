package scriptimport

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

func TestAllFormatsPreserveParagraphsWithoutNarrativeClassification(t *testing.T) {
	paragraphs := []string{"标题与设定", "没有场标题的动作。", "", "回忆继续。", "尾段"}
	var docx bytes.Buffer
	z := zip.NewWriter(&docx)
	w, _ := z.Create("word/document.xml")
	w.Write([]byte(`<w:document xmlns:w="` + wordNamespace + `"><w:body>`))
	for _, p := range paragraphs {
		w.Write([]byte(`<w:p><w:r><w:t>` + p + `</w:t></w:r></w:p>`))
	}
	w.Write([]byte(`</w:body></w:document>`))
	z.Close()
	for _, tc := range []struct {
		name string
		raw  []byte
	}{{"a.txt", []byte(strings.Join(paragraphs, "\n"))}, {"a.md", []byte(strings.Join(paragraphs, "\n"))}, {"a.docx", docx.Bytes()}} {
		doc, err := Parse(tc.name, tc.raw, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(doc.Spans) != len(paragraphs) {
			t.Fatal("paragraph lost", tc.name)
		}
		for i, p := range paragraphs {
			if doc.Spans[i].Text != p {
				t.Fatal("source rewritten", i)
			}
		}
		if doc.Spans[0].ID != "v7-p000001" {
			t.Fatal("source version lost")
		}
	}
}

func TestInvalidUploadsDoNotProduceSuccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
	}{{"a.txt", nil}, {"a.txt", []byte("  \n ")}, {"a.txt", []byte{0xff}}, {"a.docx", []byte("not zip")}, {"a.txt", bytes.Repeat([]byte("x"), d.MaxScriptBytes+1)}} {
		if _, err := Parse(tc.name, tc.raw, 1); !errors.Is(err, fault.ErrInvalidDocument) {
			t.Fatal("invalid upload accepted", tc.name, err)
		}
	}
	if _, err := Parse("a.pdf", []byte("text"), 1); !errors.Is(err, fault.ErrUnsupportedFormat) {
		t.Fatal("unsupported import accepted")
	}
}
