package lang

import (
	"fmt"
	"go.uber.org/zap"
	"strings"
)

type Handler interface {
	LibraryInstalled() ([]string, error)
	UpdateImports(content string, newImports []string) (string, int, error)
	AddCommentToTest(testCode string) string
	UninstallLibraries(installedPackages []string) error
	SetLogger(logger *zap.Logger)
}

type BaseHandler struct {
	logger *zap.Logger
}

func (b *BaseHandler) SetLogger(logger *zap.Logger) {
	b.logger = logger
}

func (b *BaseHandler) generateComment(testCode, commentPrefix, description string) string {
	comment := fmt.Sprintf("%s %s", commentPrefix, description)
	return fmt.Sprintf("%s\n%s", comment, testCode)
}

func (b *BaseHandler) extractString(output []byte) []string {
	lines := strings.Split(string(output), "\n")
	var dependencies []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			dependencies = append(dependencies, trimmed)
		}
	}
	return dependencies
}
