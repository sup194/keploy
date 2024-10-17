// Package utgen is a service that generates unit tests for a given source code file.
package utgen

import (
	"context"
	"fmt"
	utils2 "go.keploy.io/server/v2/pkg/service/utgen/utils"
	"math"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/k0kubun/pp/v3"
	"go.keploy.io/server/v2/config"
	"go.keploy.io/server/v2/pkg/models"
	"go.keploy.io/server/v2/pkg/service"
	"go.keploy.io/server/v2/utils"
	"go.uber.org/zap"
)

type Coverage struct {
	Path    string
	Format  string
	Desired float64
	Current float64
	Content string
}

type Cursor struct {
	Line        int
	Indentation int
}

type UnitTestGenerator struct {
	srcPath          string
	testPath         string
	cmd              string
	dir              string
	cov              *Coverage
	lang             string
	cur              *Cursor
	failedTests      []*models.FailedUT
	prompt           *Prompt
	ai               *AIClient
	logger           *zap.Logger
	promptBuilder    *PromptBuilder
	injector         *Injector
	maxIterations    int
	Files            []string
	tel              Telemetry
	additionalPrompt string
	totalTestCase    int
	testCasePassed   int
	testCaseFailed   int
	noCoverageTest   int
}

func NewUnitTestGenerator(
	cfg *config.Config,
	tel Telemetry,
	auth service.Auth,
	logger *zap.Logger,
) (*UnitTestGenerator, error) {
	genConfig := cfg.Gen

	generator := &UnitTestGenerator{
		srcPath:       genConfig.SourceFilePath,
		testPath:      genConfig.TestFilePath,
		cmd:           genConfig.TestCommand,
		dir:           genConfig.TestDir,
		maxIterations: genConfig.MaxIterations,
		logger:        logger,
		tel:           tel,
		ai:            NewAIClient(genConfig.Model, genConfig.APIBaseURL, genConfig.APIVersion, "", cfg.APIServerURL, auth, uuid.NewString(), logger),
		cov: &Coverage{
			Path:    genConfig.CoverageReportPath,
			Format:  genConfig.CoverageFormat,
			Desired: genConfig.DesiredCoverage,
		},
		additionalPrompt: genConfig.AdditionalPrompt,
		cur:              &Cursor{},
	}
	return generator, nil
}

func (g *UnitTestGenerator) Start(ctx context.Context) error {
	g.tel.GenerateUT()

	// Check for context cancellation before proceeding
	select {
	case <-ctx.Done():
		return fmt.Errorf("process cancelled by user")
	default:
		// Continue if no cancellation
	}

	// To find the source files if the source path is not provided
	if g.srcPath == "" {
		if err := g.runCoverage(); err != nil {
			return err
		}
		if len(g.Files) == 0 {
			return fmt.Errorf("couldn't identify the source files. Please mention source file and test file using flags")
		}
	}
	const paddingHeight = 1
	columnWidths3 := []int{29, 29, 29}
	columnWidths2 := []int{40, 40}

	for i := 0; i < len(g.Files)+1; i++ {
		newTestFile := false
		var err error

		// Respect context cancellation in each iteration
		select {
		case <-ctx.Done():
			return fmt.Errorf("process cancelled by user")
		default:
		}

		// If the source file path is not provided, iterate over all the source files and test files
		if i < len(g.Files) {
			g.srcPath = g.Files[i]
			g.testPath, err = utils2.GetTestFilePath(g.srcPath, g.dir)
			if err != nil || g.testPath == "" {
				g.logger.Error("Error getting test file path", zap.Error(err))
				continue
			}
			isCreated, err := utils2.CreateTestFile(g.testPath, g.srcPath)
			if err != nil {
				g.logger.Error("Error creating test file", zap.Error(err))
				continue
			}
			newTestFile = isCreated
		}

		g.logger.Info(fmt.Sprintf("Generating tests for file: %s", g.srcPath))
		isEmpty, err := utils.IsFileEmpty(g.testPath)
		if err != nil {
			g.logger.Error("Error checking if test file is empty", zap.Error(err))
			return err
		}
		if isEmpty {
			newTestFile = true
		}
		if !newTestFile {
			if err = g.runCoverage(); err != nil {
				return err
			}
		} else {
			g.cov.Current = 0
		}

		iterationCount := 0
		g.lang = utils2.GetCodeLanguage(g.srcPath)

		g.promptBuilder, err = NewPromptBuilder(g.srcPath, g.testPath, g.cov.Content, "", "", g.lang, g.additionalPrompt, g.logger)
		g.injector, err = NewInjectorBuilder(g.logger, g.lang)
		if err != nil {
			return err
		}
		if !isEmpty {
			if err := g.setCursor(ctx); err != nil {
				utils.LogError(g.logger, err, "Error during initial test suite analysis")
				return err
			}
		}

		// Respect context cancellation in the inner loop
		for g.cov.Current < (g.cov.Desired/100) && iterationCount < g.maxIterations {
			passedTests, noCoverageTest, failedBuild, totalTest := 0, 0, 0, 0
			select {
			case <-ctx.Done():
				return fmt.Errorf("process cancelled by user")
			default:
			}

			pp.SetColorScheme(models.GetPassingColorScheme())
			if _, err := pp.Printf("Current Coverage: %s%% for file %s\n", math.Round(g.cov.Current*100), g.srcPath); err != nil {
				utils.LogError(g.logger, err, "failed to print coverage")
			}
			if _, err := pp.Printf("Desired Coverage: %s%% for file %s\n", g.cov.Desired, g.srcPath); err != nil {
				utils.LogError(g.logger, err, "failed to print coverage")
			}

			// Check for failed tests:
			failedTestRunsValue := ""
			if g.failedTests != nil && len(g.failedTests) > 0 {
				for _, failedTest := range g.failedTests {
					code := failedTest.TestCode
					errorMessage := failedTest.ErrorMsg
					failedTestRunsValue += fmt.Sprintf("Failed Test:\n\n%s\n\n", code)
					if errorMessage != "" {
						failedTestRunsValue += fmt.Sprintf("Error message for test above:\n%s\n\n\n", errorMessage)
					} else {
						failedTestRunsValue += "\n\n"
					}
				}
			}

			g.promptBuilder.InstalledPackages, err = g.injector.libraryInstalled()
			if err != nil {
				utils.LogError(g.logger, err, "Error getting installed packages")
			}
			g.prompt, err = g.promptBuilder.BuildPrompt("test_generation", failedTestRunsValue)
			if err != nil {
				utils.LogError(g.logger, err, "Error building prompt")
				return err
			}
			g.failedTests = []*models.FailedUT{}
			testsDetails, err := g.GenerateTests(ctx)
			if err != nil {
				utils.LogError(g.logger, err, "Error generating tests")
				return err
			}

			g.logger.Info("Validating new generated tests one by one")
			g.totalTestCase += len(testsDetails.NewTests)
			totalTest = len(testsDetails.NewTests)
			for _, generatedTest := range testsDetails.NewTests {
				installedPackages, err := g.injector.libraryInstalled()
				if err != nil {
					g.logger.Warn("Error getting installed packages", zap.Error(err))
				}
				select {
				case <-ctx.Done():
					return fmt.Errorf("process cancelled by user")
				default:
				}
				err = g.ValidateTest(generatedTest, &passedTests, &noCoverageTest, &failedBuild, installedPackages)
				if err != nil {
					utils.LogError(g.logger, err, "Error validating test")
					return err
				}
			}

			iterationCount++
			if g.cov.Current < (g.cov.Desired/100) && g.cov.Current > 0 {
				if err := g.runCoverage(); err != nil {
					utils.LogError(g.logger, err, "Error running coverage")
					return err
				}
			}

			fmt.Printf("\n<=========================================>\n")
			fmt.Printf(("Tests generated in Session") + "\n")
			fmt.Printf("+-------------------------------+-------------------------------+-------------------------------+\n")
			fmt.Printf("| %s | %s | %s |\n",
				centerAlignText("Total Test Cases", 29),
				centerAlignText("Test Cases Passed", 29),
				centerAlignText("Test Cases Failed", 29))
			fmt.Printf("+-------------------------------+-------------------------------+-------------------------------+\n")
			fmt.Print(addHeightPadding(paddingHeight, 3, columnWidths3))
			fmt.Printf("| \033[33m%s\033[0m | \033[32m%s\033[0m | \033[33m%s\033[0m |\n",
				centerAlignText(fmt.Sprintf("%d", totalTest), 29),
				centerAlignText(fmt.Sprintf("%d", passedTests), 29),
				centerAlignText(fmt.Sprintf("%d", failedBuild+noCoverageTest), 29))
			fmt.Print(addHeightPadding(paddingHeight, 3, columnWidths3))
			fmt.Printf("+-------------------------------+-------------------------------+-------------------------------+\n")
			fmt.Printf(("Discarded tests in session") + "\n")
			fmt.Printf("+------------------------------------------+------------------------------------------+\n")
			fmt.Printf("| %s | %s |\n",
				centerAlignText("Build failures", 40),
				centerAlignText("No Coverage output", 40))
			fmt.Printf("+------------------------------------------+------------------------------------------+\n")
			fmt.Print(addHeightPadding(paddingHeight, 2, columnWidths2))
			fmt.Printf("| \033[35m%s\033[0m | \033[92m%s\033[0m |\n",
				centerAlignText(fmt.Sprintf("%d", failedBuild), 40),
				centerAlignText(fmt.Sprintf("%d", noCoverageTest), 40))
			fmt.Print(addHeightPadding(paddingHeight, 2, columnWidths2))
			fmt.Printf("+------------------------------------------+------------------------------------------+\n")
			fmt.Printf("<=========================================>\n")

		}

		if g.cov.Current == 0 && newTestFile {
			err := os.Remove(g.testPath)
			if err != nil {
				g.logger.Error("Error removing test file", zap.Error(err))
			}
		}

		pp.SetColorScheme(models.GetPassingColorScheme())
		if g.cov.Current >= (g.cov.Desired / 100) {
			if _, err := pp.Printf("For File %s Reached above target coverage of %s%% (Current Coverage: %s%%) in %s iterations.\n", g.srcPath, g.cov.Desired, math.Round(g.cov.Current*100), iterationCount); err != nil {
				utils.LogError(g.logger, err, "failed to print coverage")
			}
		} else if iterationCount == g.maxIterations {
			if _, err := pp.Printf("For File %s Reached maximum iteration limit without achieving desired coverage. Current Coverage: %s%%\n", g.srcPath, math.Round(g.cov.Current*100)); err != nil {
				utils.LogError(g.logger, err, "failed to print coverage")
			}
		}
	}
	fmt.Printf("\n<=========================================>\n")
	fmt.Printf(("COMPLETE TEST GENERATE SUMMARY") + "\n")
	fmt.Printf(("Total Test Summary") + "\n")

	fmt.Printf("+-------------------------------+-------------------------------+-------------------------------+\n")
	fmt.Printf("| %s | %s | %s |\n",
		centerAlignText("Total Test Cases", 29),
		centerAlignText("Test Cases Passed", 29),
		centerAlignText("Test Cases Failed", 29))

	fmt.Printf("+-------------------------------+-------------------------------+-------------------------------+\n")
	fmt.Print(addHeightPadding(paddingHeight, 3, columnWidths3))
	fmt.Printf("| \033[33m%s\033[0m | \033[32m%s\033[0m | \033[33m%s\033[0m |\n",
		centerAlignText(fmt.Sprintf("%d", g.totalTestCase), 29),
		centerAlignText(fmt.Sprintf("%d", g.testCasePassed), 29),
		centerAlignText(fmt.Sprintf("%d", g.testCaseFailed+g.noCoverageTest), 29))
	fmt.Print(addHeightPadding(paddingHeight, 3, columnWidths3))
	fmt.Printf("+-------------------------------+-------------------------------+-------------------------------+\n")

	fmt.Printf(("Discarded Cases Summary") + "\n")
	fmt.Printf("+------------------------------------------+------------------------------------------+\n")
	fmt.Printf("| %s | %s |\n",
		centerAlignText("Build failures", 40),
		centerAlignText("No Coverage output", 40))

	fmt.Printf("+------------------------------------------+------------------------------------------+\n")
	fmt.Print(addHeightPadding(paddingHeight, 2, columnWidths2))
	fmt.Printf("| \033[35m%s\033[0m | \033[92m%s\033[0m |\n",
		centerAlignText(fmt.Sprintf("%d", g.testCaseFailed), 40),
		centerAlignText(fmt.Sprintf("%d", g.noCoverageTest), 40))
	fmt.Print(addHeightPadding(paddingHeight, 2, columnWidths2))
	fmt.Printf("+------------------------------------------+------------------------------------------+\n")

	fmt.Printf("<=========================================>\n")
	return nil
}

func centerAlignText(text string, width int) string {
	text = strings.Trim(text, "\"")

	textLen := len(text)
	if textLen >= width {
		return text
	}

	leftPadding := (width - textLen) / 2
	rightPadding := width - textLen - leftPadding

	return fmt.Sprintf("%s%s%s", strings.Repeat(" ", leftPadding), text, strings.Repeat(" ", rightPadding))
}

func addHeightPadding(rows int, columns int, columnWidths []int) string {
	padding := ""
	for i := 0; i < rows; i++ {
		for j := 0; j < columns; j++ {
			if j == columns-1 {
				padding += fmt.Sprintf("| %-*s |\n", columnWidths[j], "")
			} else {
				padding += fmt.Sprintf("| %-*s ", columnWidths[j], "")
			}
		}
	}
	return padding
}

func statusUpdater(stop <-chan bool) {
	messages := []string{
		"Running tests... Please wait.",
		"Still running tests... Hang tight!",
		"Tests are still executing... Almost there!",
	}
	i := 0
	for {
		select {
		case <-stop:
			fmt.Printf("\r\033[K")
			return
		default:
			fmt.Printf("\r\033[K%s", messages[i%len(messages)])
			time.Sleep(5 * time.Second)
			i++
		}
	}
}

func (g *UnitTestGenerator) runCoverage() error {
	// Perform an initial build/test command to generate coverage report and get a baseline
	if g.srcPath != "" {
		g.logger.Info(fmt.Sprintf("Running test command to generate coverage report: '%s'", g.cmd))
	}

	stopStatus := make(chan bool)
	go statusUpdater(stopStatus)

	startTime := time.Now()

	_, _, exitCode, lastUpdatedTime, err := utils2.RunCommand(g.cmd, g.dir, g.logger)
	duration := time.Since(startTime)
	stopStatus <- true
	g.logger.Info(fmt.Sprintf("Test command completed in %v", utils2.FormatDuration(duration)))

	if err != nil {
		utils.LogError(g.logger, err, "Error running test command")
		return fmt.Errorf("error running test command: %w", err)
	}
	if exitCode != 0 {
		utils.LogError(g.logger, err, "Error running test command")
	}
	coverageProcessor := NewCoverageProcessor(g.cov.Path, utils2.GetFilename(g.srcPath), g.cov.Format)
	coverageResult, err := coverageProcessor.ProcessCoverageReport(lastUpdatedTime)
	if err != nil {
		utils.LogError(g.logger, err, "Error in coverage processing")
		return fmt.Errorf("error in coverage processing: %w", err)
	}
	g.cov.Current = coverageResult.Coverage
	g.cov.Content = coverageResult.ReportContent
	if g.srcPath == "" {
		g.Files = coverageResult.Files
	}
	return nil
}

func (g *UnitTestGenerator) GenerateTests(ctx context.Context) (*models.UTDetails, error) {
	fmt.Println("Generating Tests...")

	select {
	case <-ctx.Done():
		err := ctx.Err()
		return &models.UTDetails{}, err
	default:
	}

	response, promptTokenCount, responseTokenCount, err := g.ai.Call(ctx, g.prompt, 4096)
	if err != nil {
		return &models.UTDetails{}, err
	}

	select {
	case <-ctx.Done():
		err := ctx.Err()
		return &models.UTDetails{}, err
	default:
	}

	g.logger.Info(fmt.Sprintf("Total token used count for LLM model %s: %d", g.ai.Model, promptTokenCount+responseTokenCount))

	select {
	case <-ctx.Done():
		err := ctx.Err()
		return &models.UTDetails{}, err
	default:
	}

	testsDetails, err := utils2.UnmarshalYamlTestDetails(response)
	if err != nil {
		utils.LogError(g.logger, err, "Error unmarshalling test details")
		return &models.UTDetails{}, err
	}

	select {
	case <-ctx.Done():
		err := ctx.Err()
		return &models.UTDetails{}, err
	default:
	}

	return testsDetails, nil
}

func (g *UnitTestGenerator) setCursor(ctx context.Context) error {
	fmt.Println("Getting indentation for new Tests...")
	indentation, err := g.getIndentation(ctx)
	if err != nil {
		return fmt.Errorf("failed to analyze test headers indentation: %w", err)
	}
	fmt.Println("Getting Line number for new Tests...")
	line, err := g.getLine(ctx)
	if err != nil {
		return fmt.Errorf("failed to analyze relevant line number to insert new tests: %w", err)
	}
	g.cur.Indentation = indentation
	g.cur.Line = line
	return nil
}

func (g *UnitTestGenerator) getIndentation(ctx context.Context) (int, error) {
	indentation := -1
	allowedAttempts := 3
	counterAttempts := 0
	for indentation == -1 && counterAttempts < allowedAttempts {
		prompt, err := g.promptBuilder.BuildPrompt("indentation", "")
		if err != nil {
			return 0, fmt.Errorf("error building prompt: %w", err)
		}
		response, _, _, err := g.ai.Call(ctx, prompt, 4096)
		if err != nil {
			utils.LogError(g.logger, err, "Error calling AI model")
			return 0, err
		}
		testsDetails, err := utils2.UnmarshalYamlTestHeaders(response)
		if err != nil {
			utils.LogError(g.logger, err, "Error unmarshalling test headers")
			return 0, err
		}
		indentation, err = utils2.ConvertToInt(testsDetails.Indentation)
		if err != nil {
			return 0, fmt.Errorf("error converting test_headers_indentation to int: %w", err)
		}
		counterAttempts++
	}
	if indentation == -1 {
		return 0, fmt.Errorf("failed to analyze the test headers indentation")
	}
	return indentation, nil
}

func (g *UnitTestGenerator) getLine(ctx context.Context) (int, error) {
	line := -1
	allowedAttempts := 3
	counterAttempts := 0
	for line == -1 && counterAttempts < allowedAttempts {
		prompt, err := g.promptBuilder.BuildPrompt("insert_line", "")
		if err != nil {
			return 0, fmt.Errorf("error building prompt: %w", err)
		}
		response, _, _, err := g.ai.Call(ctx, prompt, 4096)
		if err != nil {
			utils.LogError(g.logger, err, "Error calling AI model")
			return 0, err
		}
		testsDetails, err := utils2.UnmarshalYamlTestLine(response)
		if err != nil {
			utils.LogError(g.logger, err, "Error unmarshalling test line")
			return 0, err
		}
		line, err = utils2.ConvertToInt(testsDetails.Line)
		if err != nil {
			return 0, fmt.Errorf("error converting relevant_line_number_to_insert_after to int: %w", err)
		}
		counterAttempts++
	}
	if line == -1 {
		return 0, fmt.Errorf("failed to analyze the relevant line number to insert new tests")
	}
	return line, nil
}

func (g *UnitTestGenerator) ValidateTest(generatedTest models.UT, passedTests, noCoverageTest, failedBuild *int, installedPackages []string) error {
	testCode := strings.TrimSpace(generatedTest.TestCode)
	InsertAfter := g.cur.Line
	Indent := g.cur.Indentation
	testCodeIndented := testCode
	if Indent != 0 {
		initialIndent := len(testCode) - len(strings.TrimLeft(testCode, " "))
		deltaIndent := Indent - initialIndent
		if deltaIndent > 0 {
			lines := strings.Split(testCode, "\n")
			for i, line := range lines {
				lines[i] = strings.Repeat(" ", deltaIndent) + line
			}
			testCodeIndented = strings.Join(lines, "\n")
		}
	}
	testCodeIndented = "\n" + g.injector.addCommentToTest(strings.TrimSpace(testCodeIndented)) + "\n"
	// Append the generated test to the relevant line in the test file
	originalContent, err := readFile(g.testPath)
	if err != nil {
		return fmt.Errorf("failed to read test file: %w", err)
	}
	originalContentLines := strings.Split(originalContent, "\n")
	testCodeLines := strings.Split(testCodeIndented, "\n")
	if InsertAfter > len(originalContentLines) {
		InsertAfter = len(originalContentLines)
	}
	processedTestLines := append(originalContentLines[:InsertAfter], testCodeLines...)
	processedTestLines = append(processedTestLines, originalContentLines[InsertAfter:]...)
	processedTest := strings.Join(processedTestLines, "\n")
	if err := os.WriteFile(g.testPath, []byte(processedTest), 0644); err != nil {
		return fmt.Errorf("failed to write test file: %w", err)
	}

	newInstalledPackages, err := g.injector.installLibraries(generatedTest.LibraryInstallationCode, installedPackages)
	if err != nil {
		g.logger.Debug("Error installing libraries", zap.Error(err))
	}

	// Run the test using the Runner class
	g.logger.Info(fmt.Sprintf("Running test 5 times for proper validation with the following command: '%s'", g.cmd))

	var testCommandStartTime int64
	importLen, err := g.injector.updateImports(g.testPath, generatedTest.NewImportsCode)
	if err != nil {
		g.logger.Warn("Error updating imports", zap.Error(err))
	}
	for i := 0; i < 5; i++ {

		g.logger.Info(fmt.Sprintf("Iteration no: %d", i+1))

		stdout, _, exitCode, timeOfTestCommand, _ := utils2.RunCommand(g.cmd, g.dir, g.logger)
		if exitCode != 0 {
			g.logger.Info(fmt.Sprintf("Test failed in %d iteration", i+1))
			// Test failed, roll back the test file to its original content

			if err := os.Truncate(g.testPath, 0); err != nil {
				return fmt.Errorf("failed to truncate test file: %w", err)
			}

			if err := os.WriteFile(g.testPath, []byte(originalContent), 0644); err != nil {
				return fmt.Errorf("failed to write test file: %w", err)
			}
			err = g.injector.uninstallLibraries(newInstalledPackages)

			if err != nil {
				g.logger.Warn("Error uninstalling libraries", zap.Error(err))
			}
			g.logger.Info("Skipping a generated test that failed")
			g.failedTests = append(g.failedTests, &models.FailedUT{
				TestCode: generatedTest.TestCode,
				ErrorMsg: utils2.ExtractErrorMessage(stdout),
			})
			g.testCaseFailed++
			*failedBuild++
			return nil
		}
		testCommandStartTime = timeOfTestCommand
	}

	// Check for coverage increase
	newCoverageProcessor := NewCoverageProcessor(g.cov.Path, utils2.GetFilename(g.srcPath), g.cov.Format)
	covResult, err := newCoverageProcessor.ProcessCoverageReport(testCommandStartTime)
	if err != nil {
		return fmt.Errorf("error processing coverage report: %w", err)
	}
	if covResult.Coverage <= g.cov.Current {
		g.noCoverageTest++
		*noCoverageTest++
		// Test failed to increase coverage, roll back the test file to its original content

		if err := os.Truncate(g.testPath, 0); err != nil {
			return fmt.Errorf("failed to truncate test file: %w", err)
		}

		if err := os.WriteFile(g.testPath, []byte(originalContent), 0644); err != nil {
			return fmt.Errorf("failed to write test file: %w", err)
		}

		err = g.injector.uninstallLibraries(newInstalledPackages)

		if err != nil {
			g.logger.Warn("Error uninstalling libraries", zap.Error(err))
		}

		g.logger.Info("Skipping a generated test that failed to increase coverage")
		return nil
	}
	g.testCasePassed++
	*passedTests++
	g.cov.Current = covResult.Coverage

	g.cur.Line = g.cur.Line + len(testCodeLines) + importLen

	g.logger.Info("Generated test passed and increased coverage")
	return nil
}
