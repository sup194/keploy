package utgen

import (
	"fmt"
	"go.keploy.io/server/v2/pkg/service/utgen/lang"
	"io/fs"
	"os"
	"strings"

	"go.uber.org/zap"
)

type Injector struct {
	logger      *zap.Logger
	language    string
	langHandler lang.Handler
}

func NewInjectorBuilder(logger *zap.Logger, language string) (*Injector, error) {
	var langHandler lang.Handler
	switch strings.ToLower(language) {
	case "javascript":
		langHandler = &lang.JsHandler{} // 使用 JS 特定的处理器
	case "typescript":
		langHandler = &lang.TsHandler{} // 使用 TS 特定的处理器
	case "go":
		langHandler = &lang.GoHandler{}
	case "java":
		langHandler = &lang.JavaHandler{}
	case "python":
		langHandler = &lang.PythonHandler{}
	default:
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	injectBuilder := &Injector{
		logger:      logger,
		language:    language,
		langHandler: langHandler,
	}
	return injectBuilder, nil
}

func (i *Injector) installLibraries(libraryCommands string, installedPackages []string) ([]string, error) {
	var newInstalledPackages []string
	libraryCommands = strings.TrimSpace(libraryCommands)
	if libraryCommands == "" || libraryCommands == "\"\"" {
		return newInstalledPackages, nil
	}

	commands := strings.Split(libraryCommands, "\n")
	for _, command := range commands {
		packageName := i.extractPackageName(command)

		if isStringInarray(installedPackages, packageName) {
			continue
		}
		_, _, exitCode, _, err := RunCommand(command, "", i.logger)
		if exitCode != 0 || err != nil {
			return newInstalledPackages, fmt.Errorf("failed to install library: %s", command)
		}

		installedPackages = append(installedPackages, packageName)
		newInstalledPackages = append(newInstalledPackages, packageName)
	}
	return newInstalledPackages, nil
}

func (i *Injector) extractPackageName(command string) string {
	fields := strings.Fields(command)
	if len(fields) < 3 {
		return ""
	}
	return fields[2]
}

func (i *Injector) updateImports(filePath string, imports string) (int, error) {
	newImports := strings.Split(imports, "\n")
	for i, imp := range newImports {
		newImports[i] = strings.TrimSpace(imp)
	}
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}
	content := string(contentBytes)

	var updatedContent string
	var importLength int
	updateFunc, ok := i.supportedLanguages[strings.ToLower(i.language)]
	if !ok {
		return 0, fmt.Errorf("unsupported language: %s", i.language)
	}
	updatedContent, importLength, err = updateFunc(content, newImports)
	if err != nil {
		return 0, err
	}
	err = os.WriteFile(filePath, []byte(updatedContent), fs.ModePerm)

	if err != nil {
		return 0, err
	}

	return importLength, nil
}
