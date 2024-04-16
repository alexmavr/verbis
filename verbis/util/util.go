package util

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	OllamaFile   = "ollama"
	WeaviateFile = "weaviate"
)

func GetDistPath() (string, error) {
	// Get the path of the executable
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}

	// Resolve the full path to handle symlines or relative paths
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", err
	}

	// Get the directory of the executable
	exeDir := filepath.Dir(exePath)

	// First check if we're packaged
	curDir := filepath.Join(exeDir, "../Resources")

	err = binariesPresent(curDir)
	if err != nil {
		curDir = filepath.Join(exeDir, "../dist")
		err = binariesPresent(curDir)
		if err != nil {
			return "", fmt.Errorf("binaries not found in %s: %s", curDir, err)
		}
	}

	return curDir, nil
}

func binariesPresent(path string) error {
	ollamaPath := filepath.Join(path, OllamaFile)
	_, err := os.Stat(ollamaPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("ollama binary not found")
	} else if err != nil {
		return fmt.Errorf("unable to stat ollama binary: %s", err)
	}

	weaviatePath := filepath.Join(path, WeaviateFile)
	_, err = os.Stat(weaviatePath)
	if os.IsNotExist(err) {
		return fmt.Errorf("weaviate binary not found")
	} else if err != nil {
		return fmt.Errorf("unable to stat weaviate binary: %s", err)
	}

	return nil
}

func CleanChunk(input string) string {
	// The UTF-8 BOM is sometimes present in text files, and should be removed
	bom := []byte{0xEF, 0xBB, 0xBF}
	input = strings.TrimPrefix(input, string(bom))

	// Define a regex pattern that matches characters used in major human languages
	// This includes basic Latin, Latin-1 Supplement, Greek, Cyrillic, Hebrew, Arabic, etc.
	disallowedChars := regexp.MustCompile(`[^\p{L}\p{M}\p{N}\p{P}\p{Zs}]`)
	input = disallowedChars.ReplaceAllString(input, " ")

	// Remove URLs
	urlRegex := regexp.MustCompile(`http[s]?://[^\s]+`)
	input = urlRegex.ReplaceAllString(input, "")

	// Remove non-readable text and payloads (based on patterns in your example)
	payloadRegex := regexp.MustCompile(`[a-zA-Z0-9\-_]{20,}`)
	input = payloadRegex.ReplaceAllString(input, "")

	// Remove extra whitespace
	input = regexp.MustCompile(`\s+`).ReplaceAllString(input, " ")
	input = strings.TrimSpace(input)

	return input
}

// Split the text into units (words, in this case)
func wordSplitter(sourceText string) []string {
	// Replace multiple whitespaces
	re := regexp.MustCompile(`\s+`)
	sourceText = re.ReplaceAllString(sourceText, " ")
	// Split by single whitespace
	return strings.Split(sourceText, " ")
}

func ChunkText(text string, chunkSize int, overlapFraction float64) []string {
	textWords := wordSplitter(text)
	overlapInt := int(float64(chunkSize) * overlapFraction)
	var chunks []string

	for i := 0; i < len(textWords); i += chunkSize {
		startIndex := max(i-overlapInt, 0)
		endIndex := min(i+chunkSize, len(textWords))
		chunkWords := textWords[startIndex:endIndex]
		chunk := strings.Join(chunkWords, " ")
		chunks = append(chunks, chunk)
	}

	return chunks
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
