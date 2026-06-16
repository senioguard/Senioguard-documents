package chunker

import (
	"regexp"
	"strings"
)

type WordChunker struct {
	Size    int
	Overlap int
}

func NewWord(size, overlap int) WordChunker {
	return WordChunker{Size: size, Overlap: overlap}
}

func (c WordChunker) Chunk(text string) []string {
	size := c.Size
	if size <= 0 {
		size = 1024
	}
	overlap := c.Overlap
	if overlap < 0 || overlap >= size {
		overlap = 200
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var chunks []string
	for start := 0; start < len(words); {
		end := start + size
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[start:end], " "))
		if end == len(words) {
			break
		}
		start = end - overlap
	}
	return chunks
}

type SentenceChunker struct {
	MaxChars int
}

func (c SentenceChunker) Chunk(text string) []string {
	maxChars := c.MaxChars
	if maxChars <= 0 {
		maxChars = 4000
	}
	sentences := regexp.MustCompile(`(?m)([^.!?]+[.!?]+|\S.+$)`).FindAllString(text, -1)
	var chunks []string
	var current strings.Builder
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		if current.Len() > 0 && current.Len()+len(sentence)+1 > maxChars {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
		}
		current.WriteString(sentence)
		current.WriteString(" ")
	}
	if strings.TrimSpace(current.String()) != "" {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}
	return chunks
}
