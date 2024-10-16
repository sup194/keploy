package lang

type Handler interface {
	LibraryInstalled() ([]string, error)
	UpdateImports(content string, newImports []string) (string, int, error)
	AddCommentToTest(testCode string) string
	UninstallLibraries(installedPackages []string) error
}
