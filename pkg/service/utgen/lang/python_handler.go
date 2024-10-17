package lang

import (
	"bufio"
	"fmt"
	"go.keploy.io/server/v2/pkg/service/utgen/utils"
	"go.uber.org/zap"
	"os/exec"
	"strings"
)

type PythonHandler struct {
	BaseHandler
}

func (p *PythonHandler) LibraryInstalled() ([]string, error) {
	out, err := exec.Command("pip", "freeze").Output()
	if err != nil {
		p.logger.Info("Error getting Python dependencies with `pip` command, trying `pip3` command")
		out, err = exec.Command("pip3", "freeze").Output()
		if err != nil {
			return nil, fmt.Errorf("failed to get Python dependencies: %w", err)
		}
	}
	return p.extractString(out), nil
}

func (p *PythonHandler) UninstallLibraries(installedPackages []string) error {
	for _, command := range installedPackages {
		p.logger.Info(fmt.Sprintf("Uninstalling library: %s", command))
		uninstallCommand := fmt.Sprintf("pip uninstall -y %s", command)
		p.logger.Info(fmt.Sprintf("Uninstalling library with command: %s", uninstallCommand))
		_, _, exitCode, _, err := utils.RunCommand(uninstallCommand, "", p.logger)
		if exitCode != 0 || err != nil {
			p.logger.Warn(fmt.Sprintf("Failed to uninstall library: %s", uninstallCommand), zap.Error(err))
		}
	}
	return nil
}

func (p *PythonHandler) UpdateImports(content string, newImports []string) (string, int, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	existingImportsMap := make(map[string]map[string]bool)
	codeLines := []string{}
	importLines := []string{}

	ignoredPrefixes := "# checking coverage for file - do not remove"

	for scanner.Scan() {
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)

		if trimmedLine == "" {
			continue
		}
		shouldIgnore := (strings.HasPrefix(trimmedLine, "import ") || strings.HasPrefix(trimmedLine, "from ")) && strings.Contains(trimmedLine, ignoredPrefixes)
		if shouldIgnore {
			parts := strings.Split(trimmedLine, "#")
			coreImport := strings.TrimSpace(parts[0])

			if strings.HasPrefix(coreImport, "from ") {
				fields := strings.Fields(coreImport)
				moduleName := fields[1]
				importPart := coreImport[strings.Index(coreImport, "import")+len("import "):]
				importedItems := strings.Split(importPart, ",")

				if _, exists := existingImportsMap[moduleName]; !exists {
					existingImportsMap[moduleName] = make(map[string]bool)
				}
				for _, item := range importedItems {
					cleanedItem := strings.TrimSpace(item)
					if cleanedItem != "" {
						existingImportsMap[moduleName][cleanedItem] = true
					}
				}
			}
			codeLines = append(codeLines, line)
			continue
		}

		if strings.HasPrefix(trimmedLine, "import ") || strings.HasPrefix(trimmedLine, "from ") {
			codeLines = append(codeLines, line)
		} else {
			codeLines = append(codeLines, line)
		}
	}

	for _, imp := range newImports {
		imp = strings.TrimSpace(imp)
		if imp == "\"\"" || len(imp) == 0 {
			continue
		}
		if strings.HasPrefix(imp, "from ") {
			fields := strings.Fields(imp)
			moduleName := fields[1]
			newItems := strings.Split(fields[3], ",")
			if _, exists := existingImportsMap[moduleName]; !exists {
				existingImportsMap[moduleName] = make(map[string]bool)
			}
			for _, item := range newItems {
				existingImportsMap[moduleName][strings.TrimSpace(item)] = true
			}
		} else if strings.HasPrefix(imp, "import ") {
			fields := strings.Fields(imp)
			moduleName := fields[1]
			if _, exists := existingImportsMap[moduleName]; !exists {
				existingImportsMap[moduleName] = make(map[string]bool)
			}
		}
	}
	for i, line := range codeLines {
		trimmedLine := strings.TrimSpace(line)

		if strings.HasPrefix(trimmedLine, "from ") {
			fields := strings.Fields(trimmedLine)
			moduleName := fields[1]

			if itemsMap, exists := existingImportsMap[moduleName]; exists && len(itemsMap) > 0 {
				items := utils.MapKeysToSortedSlice(itemsMap)
				importLine := fmt.Sprintf("from %s import %s", moduleName, strings.Join(items, ", "))

				if strings.Contains(trimmedLine, ignoredPrefixes) {
					importLine += " " + ignoredPrefixes
				}
				codeLines[i] = importLine
				delete(existingImportsMap, moduleName)
			}
		}
	}

	for module, itemsMap := range existingImportsMap {
		if len(itemsMap) > 0 {
			items := utils.MapKeysToSortedSlice(itemsMap)
			importLine := fmt.Sprintf("from %s import %s", module, strings.Join(items, ", "))
			importLine += " " + ignoredPrefixes
			importLines = append(importLines, importLine)
		}
	}
	nonEmptyCodeLines := []string{}
	for _, line := range codeLines {
		if strings.TrimSpace(line) != "" {
			nonEmptyCodeLines = append(nonEmptyCodeLines, line)
		}
	}

	nonEmptyImportLines := []string{}
	for _, line := range importLines {
		if strings.TrimSpace(line) != "" {
			nonEmptyImportLines = append(nonEmptyImportLines, line)
		}
	}

	updatedContent := strings.Join(nonEmptyImportLines, "\n") + "\n" + strings.Join(nonEmptyCodeLines, "\n")
	return updatedContent, len(nonEmptyImportLines), nil // TODO: check if this is correct
}

func (p *PythonHandler) AddCommentToTest(testCode string) string {
	return p.generateComment(HashCommentPrefix, DefaultTestComment, testCode)
}
