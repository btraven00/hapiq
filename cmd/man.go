package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/btraven00/hapiq/internal/version"
)

// Hidden: used by scripts/build_deb.sh to write the man pages it packages.
var genManCmd = &cobra.Command{
	Use:    "gen-man DIR",
	Short:  "Write man pages for every command to DIR",
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		rootCmd.DisableAutoGenTag = true
		escapeAngles(rootCmd)
		return doc.GenManTree(rootCmd, &doc.GenManHeader{
			Title:   "HAPIQ",
			Section: "1",
			Source:  "hapiq " + version.String(),
			Manual:  "hapiq manual",
		}, args[0])
	},
}

func init() {
	rootCmd.AddCommand(genManCmd)
}

// escapeAngles keeps "<source>" in the man pages: md2man takes it for an
// HTML tag and drops it. Indented lines are code blocks, where the escape
// would show. Only gen-man calls this, and the process ends after.
func escapeAngles(c *cobra.Command) {
	c.Use, c.Long, c.Example = escapeText(c.Use), escapeText(c.Long), escapeText(c.Example)
	for _, sub := range c.Commands() {
		escapeAngles(sub)
	}
}

func escapeText(s string) string {
	r := strings.NewReplacer("<", `\<`, ">", `\>`)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if !strings.HasPrefix(l, "\t") && !strings.HasPrefix(l, "    ") {
			lines[i] = r.Replace(l)
		}
	}
	return strings.Join(lines, "\n")
}
