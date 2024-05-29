package util

import (
	"fmt"
	"log"
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

	log.Printf("Dist directory found on %s", curDir)
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

func SanitizeString(text string) string {
	// The UTF-8 BOM is sometimes present in text files, and should be removed
	bom := []byte{0xEF, 0xBB, 0xBF}
	text = strings.TrimPrefix(text, string(bom))

	// Define a regex pattern that matches characters used in major human languages
	// This includes basic Latin, Latin-1 Supplement, Greek, Cyrillic, Hebrew, Arabic, etc.
	disallowedChars := regexp.MustCompile(`[^\p{L}\p{M}\p{N}\p{P}\p{Zs}]`)
	text = disallowedChars.ReplaceAllString(text, " ")

	// Replace internal sequences of whitespace with a single space
	spacePattern := regexp.MustCompile(`\s+`)
	text = spacePattern.ReplaceAllString(text, " ")

	// Trim leading and trailing whitespace
	// If the initial text was all whitespace, it should return an empty string
	return strings.TrimSpace(text)
}
