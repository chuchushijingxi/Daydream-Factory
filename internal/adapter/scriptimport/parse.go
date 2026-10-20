package scriptimport

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"drama-agent/internal/domain/fault"
	d "drama-agent/internal/domain/project"
)

const maxDocumentXMLBytes = 40 << 20

const maxParagraphs = 20000

const wordNamespace = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

func Parse(filename string, data []byte, version uint64) (d.ScriptSource, error) {
	if len(data) == 0 || len(data) > d.MaxScriptBytes || version == 0 {
		return d.ScriptSource{}, fmt.Errorf("%w: empty/oversized source or invalid version", fault.ErrInvalidDocument)
	}
	var paragraphs []string
	var err error
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".docx":
		paragraphs, err = docxParagraphs(data)
	case ".txt", ".md":
		if !utf8.Valid(data) {
			return d.ScriptSource{}, fmt.Errorf("%w: text must be UTF-8", fault.ErrInvalidDocument)
		}
		paragraphs = strings.Split(strings.ReplaceAll(strings.TrimPrefix(string(data), "\ufeff"), "\r\n", "\n"), "\n")
	default:
		return d.ScriptSource{}, fault.ErrUnsupportedFormat
	}
	if err != nil {
		return d.ScriptSource{}, err
	}
	if len(paragraphs) == 0 || len(paragraphs) > maxParagraphs || strings.TrimSpace(strings.Join(paragraphs, "\n")) == "" {
		return d.ScriptSource{}, fmt.Errorf("%w: no script text or too many paragraphs", fault.ErrInvalidDocument)
	}
	hash := sha256.Sum256(data)
	doc := d.ScriptSource{Version: version, Filename: filepath.Base(filename), SHA256: hex.EncodeToString(hash[:]), Spans: make([]d.SourceSpan, 0, len(paragraphs))}
	for i, text := range paragraphs {
		doc.Spans = append(doc.Spans, d.SourceSpan{ID: fmt.Sprintf("v%d-p%06d", version, i+1), Text: text})
	}
	return doc, nil
}

func docxParagraphs(data []byte) ([]string, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: cannot open DOCX zip: %v", fault.ErrInvalidDocument, err)
	}
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		if file.UncompressedSize64 > maxDocumentXMLBytes {
			return nil, fmt.Errorf("%w: document XML too large", fault.ErrInvalidDocument)
		}
		stream, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: cannot read document XML: %v", fault.ErrInvalidDocument, err)
		}
		defer stream.Close()
		decoder := xml.NewDecoder(io.LimitReader(stream, maxDocumentXMLBytes+1))
		var paragraphs []string
		var text strings.Builder
		inParagraph := false
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("%w: malformed document XML: %v", fault.ErrInvalidDocument, err)
			}
			switch value := token.(type) {
			case xml.StartElement:
				if value.Name.Space != wordNamespace {
					continue
				}
				switch value.Name.Local {
				case "p":
					inParagraph = true
					text.Reset()
				case "t":
					if inParagraph {
						var part string
						if err := decoder.DecodeElement(&part, &value); err != nil {
							return nil, fmt.Errorf("%w: malformed text run: %v", fault.ErrInvalidDocument, err)
						}
						text.WriteString(part)
					}
				case "tab":
					if inParagraph {
						text.WriteByte('\t')
					}
				case "br", "cr":
					if inParagraph {
						text.WriteByte('\n')
					}
				}
			case xml.EndElement:
				if value.Name.Space == wordNamespace && value.Name.Local == "p" && inParagraph {
					paragraphs = append(paragraphs, text.String())
					if len(paragraphs) > maxParagraphs {
						return nil, fmt.Errorf("%w: too many paragraphs", fault.ErrInvalidDocument)
					}
					inParagraph = false
				}
			}
		}
		if len(paragraphs) == 0 {
			return nil, fmt.Errorf("%w: DOCX contains no paragraphs", fault.ErrInvalidDocument)
		}
		return paragraphs, nil
	}
	return nil, fmt.Errorf("%w: missing word/document.xml", fault.ErrInvalidDocument)
}
