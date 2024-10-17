package lang

import (
	"fmt"
	"go.keploy.io/server/v2/pkg/service/utgen/utils"
	"go.uber.org/zap"
	"os/exec"
	"regexp"
	"strings"
)

type GoHandler struct {
	BaseHandler
}

func (g *GoHandler) LibraryInstalled() ([]string, error) {
	out, err := exec.Command("go", "list", "-m", "all").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get go dependencies: %w", err)
	}
	return g.extractString(out), nil
}

func (g *GoHandler) UninstallLibraries(installedPackages []string) error {
	for _, command := range installedPackages {
		g.logger.Info(fmt.Sprintf("Uninstalling library: %s", command))
		var uninstallCommand string
		uninstallCommand = fmt.Sprintf("go mod edit -droprequire %s && go mod tidy", command)
		g.logger.Info(fmt.Sprintf("Uninstalling library with command: %s", uninstallCommand))
		_, _, exitCode, _, err := utils.RunCommand(uninstallCommand, "", g.logger)
		if exitCode != 0 || err != nil {
			g.logger.Warn(fmt.Sprintf("Failed to uninstall library: %s", uninstallCommand), zap.Error(err))
		}
	}
	return nil
}

func (g *GoHandler) UpdateImports(codeBlock string, newImports []string) (string, int, error) {
	importRegex := regexp.MustCompile(`(?ms)import\s*(\([\s\S]*?\)|"[^"]+")`)
	existingImportsSet := make(map[string]bool)
	matches := importRegex.FindStringSubmatch(codeBlock)
	if matches != nil {
		importBlock := matches[0]
		importLines := strings.Split(importBlock, "\n")
		allImports := []string{}
		existingImports := g.extractGoImports(importLines, true)
		for _, imp := range existingImports {
			trimmedImp := strings.TrimSpace(imp)
			if trimmedImp != "" {
				existingImportsSet[trimmedImp] = true
			}
			allImports = append(allImports, imp)
		}
		newImports = g.extractGoImports(newImports, false)
		for _, importStatement := range newImports {
			importStatement = strings.TrimSpace(importStatement)
			if !existingImportsSet[importStatement] {
				existingImportsSet[importStatement] = true
				allImports = append(allImports, importStatement)
			}
		}
		importBlockNew := g.createGoImportBlock(allImports)
		updatedContent := importRegex.ReplaceAllString(codeBlock, importBlockNew)
		return updatedContent, len(strings.Split(importBlockNew, "\n")) - len(importLines), nil
	}
	packageRegex := regexp.MustCompile(`package\s+\w+`)

	pkgMatch := packageRegex.FindStringIndex(codeBlock)
	if pkgMatch == nil {
		return "", 0, fmt.Errorf("could not find package declaration")
	}
	newImports = g.extractGoImports(newImports, false)
	importBlock := g.createGoImportBlock(newImports)
	insertPos := pkgMatch[1]
	updatedContent := codeBlock[:insertPos] + "\n\n" + importBlock + "\n" + codeBlock[insertPos:]
	return updatedContent, len(strings.Split(importBlock, "\n")) + 1, nil
}

func (g *GoHandler) AddCommentToTest(testCode string) string {
	return g.generateComment(CommentPrefixSlash, DefaultTestComment, testCode)
}

func (g *GoHandler) extractGoImports(importLines []string, ignoreSpace bool) []string {
	imports := []string{}
	for _, line := range importLines {
		line = strings.TrimSpace(line)
		if line == "import (" || line == ")" {
			continue
		}
		if line == "" {
			if ignoreSpace {
				imports = append(imports, "")
			}
			continue
		}
		line = strings.TrimPrefix(line, "import ")
		line = strings.Trim(line, `"`)
		imports = append(imports, line)
	}
	return imports
}

func (g *GoHandler) createGoImportBlock(imports []string) string {
	importBlock := "import (\n"
	for _, importLine := range imports {
		importLine = strings.TrimSpace(importLine)
		if importLine == "" {
			importBlock += "\n"
			continue
		}
		importLine = strings.Trim(importLine, `"`)
		importBlock += fmt.Sprintf(`    "%s"`+"\n", importLine)
	}
	importBlock += ")"
	return importBlock
}
