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
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
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

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Primary: ~/.hapiqrc (TOML)
		viper.AddConfigPath(home)
		viper.SetConfigType("toml")
		viper.SetConfigName(".hapiqrc")

		// Fallback paths merged after the first-match read.
		viper.AddConfigPath(home)
		viper.AddConfigPath("/etc/hapiq")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		// Try legacy ~/.hapiq.yaml before giving up.
		home, _ := os.UserHomeDir()
		viper.SetConfigType("yaml")
		viper.SetConfigName(".hapiq")
		viper.AddConfigPath(home)
		if err2 := viper.ReadInConfig(); err2 == nil && !quiet {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
		return
	}

	if !quiet {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
