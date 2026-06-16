package extractor

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"regexp"
	"strings"

	"senioguard-documents/internal/module"
)

type Registry struct {
	byMIME map[string]module.Extractor
}

func NewRegistry(extractors ...module.Extractor) Registry {
	r := Registry{byMIME: map[string]module.Extractor{}}
	for _, e := range extractors {
		for _, mime := range e.SupportedMIMEs() {
			r.byMIME[mime] = e
		}
	}
	return r
}

func (r Registry) Extract(mime string, reader io.Reader) (string, error) {
	if e, ok := r.byMIME[mime]; ok {
		return e.Extract(reader)
	}
	if idx := strings.Index(mime, ";"); idx > -1 {
		if e, ok := r.byMIME[strings.TrimSpace(mime[:idx])]; ok {
			return e.Extract(reader)
		}
	}
	return "", fmt.Errorf("unsupported MIME type %q", mime)
}

func DetectMIME(filename, declared string) string {
	if declared != "" && declared != "application/octet-stream" {
		return declared
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		if t := mime.TypeByExtension(filepath.Ext(filename)); t != "" {
			return t
		}
		return "application/octet-stream"
	}
}

type PlainTextExtractor struct{}

func (PlainTextExtractor) Extract(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	return string(data), err
}

func (PlainTextExtractor) SupportedMIMEs() []string {
	return []string{"text/plain"}
}

type MarkdownExtractor struct{}

func (MarkdownExtractor) Extract(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	return string(data), err
}

func (MarkdownExtractor) SupportedMIMEs() []string {
	return []string{"text/markdown", "text/x-markdown"}
}

type PDFExtractor struct{}

func (PDFExtractor) Extract(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, match := range regexp.MustCompile(`\(([^()]*)\)`).FindAllSubmatch(data, -1) {
		text := strings.ReplaceAll(string(match[1]), `\)`, ")")
		text = strings.ReplaceAll(text, `\(`, "(")
		b.WriteString(text)
		b.WriteString(" ")
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", fmt.Errorf("PDF text extraction found no plain text; use OCR or a richer PDF parser module")
	}
	return strings.TrimSpace(b.String()), nil
}

func (PDFExtractor) SupportedMIMEs() []string {
	return []string{"application/pdf"}
}

type DOCXExtractor struct{}

func (DOCXExtractor) Extract(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		return docxText(rc)
	}
	return "", fmt.Errorf("DOCX missing word/document.xml")
}

func (DOCXExtractor) SupportedMIMEs() []string {
	return []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"}
}

func docxText(r io.Reader) (string, error) {
	decoder := xml.NewDecoder(r)
	var b strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				var value string
				if err := decoder.DecodeElement(&value, &t); err != nil {
					return "", err
				}
				b.WriteString(value)
			}
			if t.Name.Local == "p" {
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}
