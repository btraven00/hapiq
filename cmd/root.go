package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/btraven00/hapiq/pkg/cache"
	"github.com/btraven00/hapiq/pkg/downloaders/experimenthub"
)

var (
	cfgFile string
	quiet   bool
	output  string
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "hapiq",
	Short: "A CLI tool for downloading datasets from scientific repositories",
	Long: `Hapiq downloads datasets from scientific data repositories with full
provenance tracking. Point it at a source and ID; it handles the rest.

Supported sources: geo, figshare, zenodo, ensembl

"Hapiq" means "the one who fetches" in Quechua.`,
	SilenceUsage: true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.hapiqrc)")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "quiet output (suppress verbose messages)")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "human", "output format (human, json)")

	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
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
	viper.SetConfigFile("/etc/hapiq/config.toml")
	if err := viper.ReadInConfig(); err == nil && !quiet {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
