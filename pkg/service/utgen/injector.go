package utgen

import (
	"fmt"
	"go.keploy.io/server/v2/pkg/service/utgen/lang"
	"go.keploy.io/server/v2/pkg/service/utgen/utils"
	"io/fs"
	"os"
	"strings"

	"go.uber.org/zap"
)

type Injector struct {
	logger      *zap.Logger
	langHandler lang.Handler
}

func NewInjectorBuilder(logger *zap.Logger, language string) (*Injector, error) {
	injectBuilder := &Injector{
		logger: logger,
	}
	langHandler, err := injectBuilder.getLangHandler(language)
	if err != nil {
		return nil, err
	}
	injectBuilder.langHandler = langHandler
	return injectBuilder, nil
}

func (i *Injector) libraryInstalled() ([]string, error) {
	return i.langHandler.LibraryInstalled()
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

		if utils.IsStringInarray(installedPackages, packageName) {
			continue
		}
		_, _, exitCode, _, err := utils.RunCommand(command, "", i.logger)
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
	updatedContent, importLength, err = i.langHandler.UpdateImports(content, newImports)
	if err != nil {
		return 0, err
	}
	err = os.WriteFile(filePath, []byte(updatedContent), fs.ModePerm)
	if err != nil {
		return 0, err
	}
	return importLength, nil
}

func (i *Injector) addCommentToTest(testCode string) string {
	return i.langHandler.AddCommentToTest(testCode)
}

func (i *Injector) uninstallLibraries(installedPackages []string) error {
	return i.langHandler.UninstallLibraries(installedPackages)
}

func (i *Injector) getLangHandler(language string) (lang.Handler, error) {
	var langHandler lang.Handler
	switch strings.ToLower(language) {
	case "javascript":
		langHandler = &lang.JsHandler{}
	case "typescript":
		langHandler = &lang.TsHandler{}
	case "go":
		langHandler = &lang.GoHandler{}
	case "java":
		langHandler = &lang.JavaHandler{}
	case "python":
		langHandler = &lang.PythonHandler{}
	default:
		return nil, fmt.Errorf("unsupported language: %s", language)
	}
	langHandler.SetLogger(i.logger)
	return langHandler, nil
}
