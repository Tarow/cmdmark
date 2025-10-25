package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	fzf "github.com/junegunn/fzf/src"
)

const (
	fieldDelimiter = "\t"
)

var (
	varPatternRegex = regexp.MustCompile(`{{\s*([\w-]+)\s*}}`)
)

func toChan[T any](s []T) chan T {
	ch := make(chan T, len(s))
	defer close(ch)
	for _, e := range s {
		ch <- e
	}
	return ch
}

func formatLine(fields ...string) string {
	return strings.Join(fields, fieldDelimiter)
}

func placeholderRegex(varName string) *regexp.Regexp {
	return regexp.MustCompile(`{{\s*` + regexp.QuoteMeta(varName) + `\s*}}`)
}

func executeCommand(cmdStr string, output chan string) {
	defer close(output)
	cmd := exec.Command("sh", "-c", cmdStr)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("failed to get stdout pipe: %v\n", err)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("failed to start command %q: %v\n", cmdStr, err)
		return
	}

	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			output <- line
		}
	}
	if err := sc.Err(); err != nil {
		log.Printf("reading command output %q: %v\n", cmdStr, err)
	}
	if err = cmd.Wait(); err != nil {
		log.Printf("command '%q' finished with error: %v\n", cmdStr, err)
	}
}

func replaceVariables(template string, varNames []string, vars map[string]VarDefinition) (string, error) {
	result := template

	for idx, varName := range varNames {
		var varDef *VarDefinition
		if def, exists := vars[varName]; exists {
			varDef = &def
		}

		val, err := promptVariable(varName, varDef, result, idx == len(varNames)-1)
		if err != nil {
			return "", fmt.Errorf("failed to read variable %q: %w", varName, err)
		}

		re := placeholderRegex(varName)
		result = re.ReplaceAllString(result, val)
	}

	return result, nil
}

func promptVariable(varName string, varDef *VarDefinition, currentCommand string, isLast bool) (string, error) {
	var err error
	options := make(chan string)
	delimiter := " "
	var label string
	var preview string

	fzfArgs := []string{}
	keyBindLabels := []string{"Ctrl-C: Quit", "Esc: Skip"}
	if isLast {
		keyBindLabels = append(keyBindLabels, "Ctrl-E: Execute")

	}

	escapedCurrentCommand := strings.ReplaceAll(currentCommand, `'`, `\'`)
	if varDef == nil {
		close(options)

		fzfArgs = append(fzfArgs, printQueryArg())
		label = fmt.Sprintf("Enter {{%s}}", varName)
		preview = fmt.Sprintf(
			`[ -z {q} ] && echo '%[1]s' || echo '%[1]s' | sed -E "s|\\{\\{\\s*%s\\s*\\}\\}|$(printf "%%s" {q})|g"`,
			escapedCurrentCommand, varName,
		)
	} else {
		delimiter = varDef.Delimiter
		label = fmt.Sprintf("Select %s", varName)
		preview = fmt.Sprintf(
			`echo '%[1]s' | sed -E "s|\\{\\{\\s*%s\\s*\\}\\}|$([ -z $(echo {+}) ] && printf "%%s\n" {q} || printf "%%s\n" {+} | paste -sd '%s')|g"`,
			escapedCurrentCommand, varName, delimiter,
		)
		if varDef.Multi {
			fzfArgs = append(fzfArgs, multiArg())
		}

		if len(varDef.Options) > 0 {
			options = toChan(varDef.Options)
		} else if varDef.OptionsCmd != "" {
			go executeCommand(varDef.OptionsCmd, options)
		}
	}
	fzfArgs = append(fzfArgs, promptArg(label+": "), previewArg(preview))

	selected, _, err := invokeFzf(options, fzfArgs)

	if err != nil {
		return "", fmt.Errorf("failed to select variable %q: %w", varName, err)
	}
	return strings.Join(selected, delimiter), nil
}

func selectCommand(cmds []Command, commandVariables map[int][]string) (*Command, int, error) {
	input := make(chan string, len(cmds))
	go func() {
		defer close(input)
		for idx, cmd := range cmds {
			inputLabel := []string{"Ctrl-C: Quit"}
			executeAction := "ignore"

			// If command has no vars, direct execution can be triggered
			if len(commandVariables[idx]) == 0 {
				inputLabel = append(inputLabel, "Ctrl-E: Execute")
				executeAction = "become(sh -c {3})"
			}

			// Cmd Index | Cmd Title | Cmd | Keybinds | Execute Action
			input <- formatLine(strconv.Itoa(idx), cmd.Title, cmd.Cmd,
				strings.Join(inputLabel, " | "), executeAction)
		}
	}()

	args := []string{
		withNthArg("{2}"),
		acceptNthArg("{1}"),
		previewArg("echo {3}"),
		bindingArg("focus:transform-input-label:echo {4}"),
		bindingArg("ctrl-e:transform:echo {5}"),
		//inputLabelArg("Select Command"),
		listLabelArg("Commands"),
		inputLabelArg("Ctrl-C: Quit"),
	}
	selection, rc, err := invokeFzf(input, args)
	if err != nil {
		return nil, -1, fmt.Errorf("command selection failed: %w", err)
	}

	if len(selection) == 0 || rc == fzf.ExitInterrupt {
		return nil, -1, nil
	}

	commandIndex, err := strconv.Atoi(selection[0])
	if err != nil {
		return nil, -1, err
	}
	if commandIndex >= len(cmds) {
		return nil, -1, fmt.Errorf("command index not found: %v", commandIndex)
	}

	cmd := cmds[commandIndex]
	return &cmd, commandIndex, nil
}

func extractVariableNames(template string) []string {
	variabes := varPatternRegex.FindAllStringSubmatch(template, -1)

	uniqueVars := make([]string, 0, len(variabes))

	seen := make(map[string]bool, len(variabes))
	for _, elem := range variabes {
		if len(elem) > 1 {
			varName := strings.TrimSpace(elem[1])
			if !seen[varName] {
				uniqueVars = append(uniqueVars, varName)
				seen[varName] = true
			}
		}
	}

	return uniqueVars
}

func run() error {
	var config Config
	var err error

	if len(os.Args) > 1 {
		config, err = loadConfig(os.Args[1])
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	} else {
		return errors.New("no config file provided")
	}

	commandVariables := map[int][]string{}
	for idx, cmd := range config.Commands {
		commandVariables[idx] = extractVariableNames(cmd.Cmd)
	}
	fmt.Printf("%+v", commandVariables)

	cmd, cmdIdx, err := selectCommand(config.Commands, commandVariables)
	if err != nil || cmd == nil {
		return err
	}

	mergedVars := mergeVars(config.GlobalVars, cmd.Vars)

	fullCmd, err := replaceVariables(cmd.Cmd, commandVariables[cmdIdx], mergedVars)
	if err != nil {
		return err
	}
	fmt.Println(fullCmd)

	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("Error: %v\n", err)
	}
}
