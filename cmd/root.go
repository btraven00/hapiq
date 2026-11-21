package cmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/btraven00/hapiq/pkg/cache"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
	"github.com/btraven00/hapiq/pkg/downloaders/experimenthub"
)

var (
	cfgFile string
	headers []string
	quiet   bool
	output  string
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "hapiq",
	Short: "A CLI tool for downloading datasets from scientific repositories",
	Long: `Hapiq downloads datasets from scientific data repositories with full
provenance tracking. Point it at a source and ID; it handles the rest.

Run "hapiq downloaders" for the supported sources (GEO, SRA/ENA, Zenodo,
Figshare, Ensembl, CZI, HCA and more).

"Hapiq" means "the one who fetches" in Quechua.`,
	SilenceUsage:  true,
	SilenceErrors: true, // main prints the error; cobra printing it too doubled it
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Every HTTP client that uses the default transport now sends hapiq's
	// User-Agent and the --header values.
	http.DefaultTransport = common.HeaderTransport(http.DefaultTransport)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.hapiqrc)")
	rootCmd.PersistentFlags().StringArrayVarP(&headers, "header", "H", nil,
		"extra HTTP header as \"Name: value\", sent with every request (repeatable, like curl -H); "+
			"it is sent to every host contacted, redirects included")
	rootCmd.PersistentPreRunE = func(*cobra.Command, []string) error { return common.SetExtraHeaders(headers) }
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "quiet output (suppress verbose messages)")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "human", "output format (human, json)")
}

// initConfig reads in config file and ENV variables if set.
// Search order (first match wins):
//  1. --config <path> flag
//  2. $HOME/.hapiqrc  (TOML)
//  3. $HOME/.hapiq.yaml (legacy YAML)
//  4. /etc/hapiq/config.toml
func initConfig() {
	cache.RegisterDefaults()
	experimenthub.RegisterDefaults()

	viper.AutomaticEnv()

	if cfgFile != "" {
		// Sniff the type from the extension. The documented hapiq config name
		// is the extensionless "~/.hapiqrc" (TOML), and viper cannot infer
		// the type without help — default to TOML for unknown extensions.
		switch strings.ToLower(filepath.Ext(cfgFile)) {
		case ".yaml", ".yml":
			viper.SetConfigType("yaml")
		case ".json":
			viper.SetConfigType("json")
		default:
			viper.SetConfigType("toml")
		}
		viper.SetConfigFile(cfgFile)
		if err := viper.ReadInConfig(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: config file %s: %v\n", cfgFile, err)
		} else if !quiet {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
		return
	}

	home, err := os.UserHomeDir()
	cobra.CheckErr(err)

	// 1. ~/.hapiqrc (TOML)
	viper.SetConfigType("toml")
	viper.SetConfigName(".hapiqrc")
	viper.AddConfigPath(home)
	if err := viper.ReadInConfig(); err == nil {
		if !quiet {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
		return
	}

	// 2. ~/.hapiq.yaml (legacy YAML)
	viper.SetConfigType("yaml")
	viper.SetConfigName(".hapiq")
	viper.AddConfigPath(home)
	if err := viper.ReadInConfig(); err == nil {
		if !quiet {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
		return
	}

	// 3. /etc/hapiq/config.toml (system-wide)
	viper.SetConfigType("toml")
	viper.SetConfigFile("/etc/hapiq/config.toml")
	if err := viper.ReadInConfig(); err == nil && !quiet {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
