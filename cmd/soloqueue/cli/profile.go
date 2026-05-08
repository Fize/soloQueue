package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/prompt"
)

// PromptProfileQuestions runs the interactive onboarding questionnaire before TUI startup.
// It collects Name / Gender / Personality / CommStyle and writes the result to the role's soul.md.
// Preset characters are no longer bundled in the binary — see docs/roles/ for example souls.
func PromptProfileQuestions() prompt.ProfileAnswers {
	answers := prompt.DefaultProfileAnswers()

	fmt.Println(prompt.ProfilePromptText())
	fmt.Println()

	answers.Name = ReadLineWithDefault("1. What should we call your assistant?", answers.Name)
	answers.Gender = ReadLineWithDefault("2. Assistant gender (male/female)?", answers.Gender)
	answers.Personality = ReadLineWithDefault("3. Personality (strict/playful/gentle/direct/custom)?", answers.Personality)
	answers.CommStyle = ReadLineWithDefault("4. Communication style (brief/detailed/casual/formal)?", answers.CommStyle)

	return answers
}

// ReadLineWithDefault reads a line of input, returning the default if the line is empty.
func ReadLineWithDefault(prompt, def string) string {
	fmt.Printf("%s [%s] ", prompt, def)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			return input
		}
	}
	return def
}
