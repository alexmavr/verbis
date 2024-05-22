package connectors

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"

	"github.com/verbis-ai/verbis/verbis/util"
)

const (
	pdfToTextPath = "pdftotext/pdftoText"
)

var (
	SupportedMimeTypes = map[string]bool{
		"application/pdf": true,
		//		"image/jpeg":      true,
		//		"image/png":       true,
		//		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
		//		"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true,
		//		"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
	}
)

type ParseRequest struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type UnstructuredChunk struct {
	Index   int    `json:"index"`
	Content string `json:"string"`
}

func ParseBinaryFile(ctx context.Context, request *ParseRequest) (string, error) {
	// Execute the Python script and pass JSON data to stdin
	distPath, err := util.GetDistPath()
	if err != nil {
		return "", fmt.Errorf("failed to get dist path: %v", err)
	}

	path := filepath.Join(distPath, pdfToTextPath)
	cmd := exec.CommandContext(ctx, path, "-layout", request.Path, "-")
	output, err := cmd.CombinedOutput()
	log.Print(string(output))
	if err != nil {
		log.Print(string(output))
		return "", fmt.Errorf("error executing script: %v", err)
	}

	return util.CleanWhitespace(string(output)), nil
}
