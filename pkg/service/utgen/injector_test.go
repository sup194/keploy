package utgen

import (
	"github.com/go-python/gpython/compile"
	"github.com/go-python/gpython/py"
	"sort"
	"strings"
	"testing"
)

func TestUpdatePythonImports(t *testing.T) {
	injector := &Injector{}

	tests := []struct {
		name       string
		content    string
		newImports []string
		expected   string
	}{
		{
			name: "Basic Merge of from and import statements",
			content: `from math import sqrt

def func():
    return sqrt(4)
`,
			newImports: []string{
				"from math import pow, ceil",
				"import os",
			},
			expected: `from math import ceil, pow, sqrt
import os

def func():
    return sqrt(4)
`,
		},
		{
			name: "Merge existing and new imports with comments",
			content: `from math import sqrt  # checking coverage for file - do not remove
from random import randint  # checking coverage for file - do not remove

def func():
    return randint(0, sqrt(4))
`,
			newImports: []string{
				"from math import pow, ceil",
				"from random import shuffle",
				"import os",
			},
			expected: `from math import ceil, pow, sqrt # checking coverage for file - do not remove
from random import randint, shuffle # checking coverage for file - do not remove
import os

def func():
    return randint(0, sqrt(4))
`,
		},
		{
			name: "No import lines in content",
			content: `def func():
    return 42
`,
			newImports: []string{
				"import os",
			},
			expected: `import os

def func():
    return 42
`,
		},
		{
			name: "Test for commented imports",
			content: `from math import sqrt  # checking coverage for file - do not remove

def func():
    return sqrt(4)
`,
			newImports: []string{
				"from math import pow, ceil",
			},
			expected: `from math import ceil, pow, sqrt # checking coverage for file - do not remove

def func():
    return sqrt(4)
`,
		},
		{
			name: "Test new imports without original imports",
			content: `def func():
    return 42
`,
			newImports: []string{
				"from math import sqrt",
				"import os",
			},
			expected: `from math import sqrt
import os

def func():
    return 42
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updatedContent, err := injector.updatePythonImports(tt.content, tt.newImports)
			if err != nil {
				t.Fatalf("Expected no error, but got: %v", err)
			}

			// 校验生成的 Python 代码是否语法正确 (使用 gpython)
			err = checkPythonSyntaxWithGpython(updatedContent)
			if err != nil {
				t.Fatalf("Generated Python code has syntax errors: %v", err)
			}

			// Split the imports and code lines for both updatedContent and expected
			updatedLines := strings.Split(updatedContent, "\n")
			expectedLines := strings.Split(tt.expected, "\n")

			// Separate import lines and code lines for comparison
			updatedImports, updatedCode := splitImportsAndCode(updatedLines)
			expectedImports, expectedCode := splitImportsAndCode(expectedLines)

			// 排序并忽略空行
			updatedImports = filterEmptyLines(updatedImports)
			expectedImports = filterEmptyLines(expectedImports)
			sort.Strings(updatedImports)
			sort.Strings(expectedImports)

			// Compare imports ignoring order and empty lines
			if !equalLines(updatedImports, expectedImports) {
				t.Errorf("Imports mismatch\nExpected:\n%s\nGot:\n%s", strings.Join(expectedImports, "\n"), strings.Join(updatedImports, "\n"))
			}

			// Compare code (ignoring import lines)
			updatedCode = filterEmptyLines(updatedCode)
			expectedCode = filterEmptyLines(expectedCode)
			if !equalLines(updatedCode, expectedCode) {
				t.Errorf("Code mismatch\nExpected:\n%s\nGot:\n%s", strings.Join(expectedCode, "\n"), strings.Join(updatedCode, "\n"))
			}
		})
	}
}

// splitImportsAndCode separates import lines from code lines
func splitImportsAndCode(lines []string) (imports []string, code []string) {
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "from ") || strings.HasPrefix(trimmedLine, "import ") {
			imports = append(imports, trimmedLine)
		} else if trimmedLine != "" {
			code = append(code, trimmedLine)
		}
	}
	return imports, code
}

// equalLines compares two slices of strings and checks if they contain the same elements
func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// filterEmptyLines removes empty lines from the slice
func filterEmptyLines(lines []string) []string {
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}

// checkPythonSyntaxWithGpython checks Python syntax using gpython (AST parsing)
func checkPythonSyntaxWithGpython(content string) error {
	_, err := compile.Compile(content, "", py.ExecMode, 0, false)
	if err != nil {
		return err
	}
	return nil
}
