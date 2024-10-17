package lang

import (
	"fmt"
	"go.keploy.io/server/v2/pkg/service/utgen/utils"
	"go.uber.org/zap"
	"os/exec"
	"regexp"
	"strings"
)

type TsHandler struct {
	BaseHandler
}

func (ts *TsHandler) LibraryInstalled() ([]string, error) {
	cmd := exec.Command("sh", "-c", "npm list --depth=0 --parseable | sed 's|.*/||'")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get JavaScript/TypeScript dependencies: %w", err)
	}
	return ts.extractString(out), nil
}

func (ts *TsHandler) UninstallLibraries(installedPackages []string) error {
	for _, command := range installedPackages {
		ts.logger.Info(fmt.Sprintf("Uninstalling library: %s", command))
		uninstallCommand := fmt.Sprintf("npm uninstall %s", command)
		ts.logger.Info(fmt.Sprintf("Uninstalling library with command: %s", uninstallCommand))
		_, _, exitCode, _, err := utils.RunCommand(uninstallCommand, "", ts.logger)
		if exitCode != 0 || err != nil {
			ts.logger.Warn(fmt.Sprintf("Failed to uninstall library: %s", uninstallCommand), zap.Error(err))
		}
	}
	return nil
}

func (ts *TsHandler) UpdateImports(importedContent string, newImports []string) (string, int, error) {
	importRegex := regexp.MustCompile(`(?m)^import\s+.*?;`)
	existingImportsSet := make(map[string]bool)

	existingImports := importRegex.FindAllString(importedContent, -1)
	for _, imp := range existingImports {
		existingImportsSet[imp] = true
	}

	for _, imp := range newImports {
		imp = strings.TrimSpace(imp)
		if importRegex.MatchString(imp) {
			existingImportsSet[imp] = true
		}
	}

	allImports := make([]string, 0, len(existingImportsSet))
	for imp := range existingImportsSet {
		allImports = append(allImports, imp)
	}
	importSection := strings.Join(allImports, "\n")

	updatedContent := importRegex.ReplaceAllString(importedContent, "")
	updatedContent = strings.Trim(updatedContent, "\n")
	lines := strings.Split(updatedContent, "\n")
	cleanedLines := []string{}
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine != "" {
			cleanedLines = append(cleanedLines, line)
		}
	}
	updatedContent = strings.Join(cleanedLines, "\n")
	updatedContent = importSection + "\n" + updatedContent
	importLength := len(strings.Split(updatedContent, "\n")) - len(strings.Split(importedContent, "\n"))
	if importLength < 0 {
		importLength = 0
	}
	return updatedContent, importLength, nil
}

func (ts *TsHandler) AddCommentToTest(testCode string) string {
	return ts.generateComment(CommentPrefixSlash, DefaultTestComment, testCode)
}
