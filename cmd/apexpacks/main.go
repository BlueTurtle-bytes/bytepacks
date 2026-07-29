package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "apexpacks",
		Short: "Build secure OCI images from language profiles",
		Long: `apexpacks builds minimal, secure OCI images using melange and apko.

Language profiles (profiles/*.yaml) define how each language is detected,
built, and assembled into an image. No Dockerfiles required.

Quick start:
  apexpacks doctor                # check required tools are installed
  apexpacks detect .              # detect the language in current directory
  apexpacks build .               # build an OCI image
  apexpacks profiles              # list available language profiles`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		detectCmd(),
		buildCmd(),
		scanCmd(),
		patchCmd(),
		normalizeSBOMCmd(),
		profilesCmd(),
		doctorCmd(),
		versionCmd(),
	)

	return root
}
