package lang

import (
	"fmt"
	"go.keploy.io/server/v2/pkg/service/utgen"
	"go.uber.org/zap"
	"os/exec"
	"regexp"
	"strings"
)

type JavaHandler struct {
	logger *zap.Logger
}

func (j *JavaHandler) LibraryInstalled() ([]string, error) {
	out, err := exec.Command("mvn", "dependency:list", "-DincludeScope=compile", "-Dstyle.color=never", "-B").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get Java dependencies: %w", err)
	}
	return j.extractJavaDependencies(out), nil
}

func (j *JavaHandler) UninstallLibraries(installedPackages []string) error {
	for _, command := range installedPackages {
		j.logger.Info(fmt.Sprintf("Uninstalling library: %s", command))
		uninstallCommand := fmt.Sprintf("mvn dependency:purge-local-repository -DreResolve=false -Dinclude=%s", command)
		j.logger.Info(fmt.Sprintf("Uninstalling library with command: %s", uninstallCommand))
		_, _, exitCode, _, err := utgen.RunCommand(uninstallCommand, "", j.logger)
		if exitCode != 0 || err != nil {
			j.logger.Warn(fmt.Sprintf("Failed to uninstall library: %s", uninstallCommand), zap.Error(err))
		}
	}
	return nil
}

func (j *JavaHandler) UpdateImports(codeContent string, newImports []string) (string, int, error) {
	importRegex := regexp.MustCompile(`(?m)^import\s+.*?;`)
	existingImportsSet := make(map[string]bool)
	existingImportMatches := importRegex.FindAllStringIndex(codeContent, -1)

	for _, match := range existingImportMatches {
		imp := codeContent[match[0]:match[1]]
		existingImportsSet[imp] = true
	}

	importsToAdd := []string{}
	for _, importStatement := range newImports {
		importStatement = strings.ReplaceAll(importStatement, "-", "")
		importStatement = strings.TrimSpace(importStatement)
		importStatement = strings.Trim(importStatement, "\"")
		if importRegex.MatchString(importStatement) && !existingImportsSet[importStatement] {
			existingImportsSet[importStatement] = true
			importsToAdd = append(importsToAdd, importStatement)
		}
	}
	if len(importsToAdd) > 0 {
		insertPos := 0
		if len(existingImportMatches) > 0 {
			lastImportMatch := existingImportMatches[len(existingImportMatches)-1]
			insertPos = lastImportMatch[1] // position after last existing import
		} else {
			packageRegex := regexp.MustCompile(`(?m)^package\s+.*?;`)
			pkgMatch := packageRegex.FindStringIndex(codeContent)
			if pkgMatch != nil {
				insertPos = pkgMatch[1]
			} else {
				insertPos = 0
			}
		}

		importedContent := "\n" + strings.Join(importsToAdd, "\n") + "\n"

		updatedContent := codeContent[:insertPos] + importedContent + codeContent[insertPos:]

		return updatedContent, len(importsToAdd), nil
	}
	return codeContent, 0, nil
}

func (j *JavaHandler) AddCommentToTest(testCode string) string {
	return utgen.GenerateComment(utgen.CommentPrefixSlash, utgen.DefaultTestComment, testCode)
}

func (j *JavaHandler) extractJavaDependencies(output []byte) []string {
	lines := strings.Split(string(output), "\n")
	var dependencies []string
	inDependencySection := false

	depRegex := regexp.MustCompile(`^\[INFO\]\s+[\+\|\\\-]{1,2}\s+([\w\.\-]+:[\w\.\-]+):jar:([\w\.\-]+):([\w\.\-]+)`)

	for _, line := range lines {
		cleanedLine := strings.TrimSpace(line)
		if strings.HasPrefix(cleanedLine, "[INFO]") {
			cleanedLine = "[INFO]" + strings.TrimSpace(cleanedLine[6:])
		}
		if strings.Contains(cleanedLine, "maven-dependency-plugin") && strings.Contains(cleanedLine, ":list") {
			inDependencySection = true
			continue
		}

		if inDependencySection && (strings.Contains(cleanedLine, "BUILD SUCCESS") || strings.Contains(cleanedLine, "---")) {
			inDependencySection = false
			continue
		}

		if inDependencySection && strings.HasPrefix(cleanedLine, "[INFO]") {
			matches := depRegex.FindStringSubmatch(cleanedLine)
			if len(matches) >= 4 {
				groupArtifact := matches[1]
				version := matches[2]
				dep := fmt.Sprintf("%s:%s", groupArtifact, version)
				dependencies = append(dependencies, dep)
			} else {
				cleanedLine = strings.TrimPrefix(cleanedLine, "[INFO]")
				cleanedLine = strings.TrimSpace(cleanedLine)

				cleanedLine = strings.TrimPrefix(cleanedLine, "+-")
				cleanedLine = strings.TrimPrefix(cleanedLine, "\\-")
				cleanedLine = strings.TrimPrefix(cleanedLine, "|")

				cleanedLine = strings.TrimSpace(cleanedLine)

				depParts := strings.Split(cleanedLine, ":")
				if len(depParts) >= 5 {
					dep := fmt.Sprintf("%s:%s:%s", depParts[0], depParts[1], depParts[3])
					dependencies = append(dependencies, dep)
				}
			}
		}
	}
	return dependencies
}
