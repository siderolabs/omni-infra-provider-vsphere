// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package main is the root cmd of the provider script.
package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/siderolabs/omni/client/pkg/client"
	"github.com/siderolabs/omni/client/pkg/infra"
	"github.com/spf13/cobra"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/session/keepalive"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"

	"github.com/siderolabs/omni-infra-provider-vsphere/internal/pkg/config"
	"github.com/siderolabs/omni-infra-provider-vsphere/internal/pkg/provider"
	"github.com/siderolabs/omni-infra-provider-vsphere/internal/pkg/provider/meta"
)

//go:embed data/schema.json
var schema string

//go:embed data/icon.svg
var icon []byte

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:          "provider",
	Short:        "vSphere Omni infrastructure provider",
	Long:         `Connects to Omni as an infra provider and manages VMs in vSphere`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		loggerConfig := zap.NewProductionConfig()

		logger, err := loggerConfig.Build(
			zap.AddStacktrace(zapcore.ErrorLevel),
		)
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		if cfg.omniAPIEndpoint == "" {
			return fmt.Errorf("omni-api-endpoint flag is not set")
		}

		var config config.Config

		configRaw, err := os.Open(cfg.configFile)
		if err != nil {
			return fmt.Errorf("failed to read vSphere config file %q", cfg.configFile)
		}

		decoder := yaml.NewDecoder(configRaw)

		if err = decoder.Decode(&config); err != nil {
			return fmt.Errorf("failed to read vSphere config file %q", cfg.configFile)
		}

		u, err := url.Parse(config.VSphere.URI)
		if err != nil {
			return fmt.Errorf("bad vSphere connection URI: %s", config.VSphere.URI)
		}

		u.User = url.UserPassword(config.VSphere.User, config.VSphere.Password)

		vsphereClient, err := govmomi.NewClient(cmd.Context(), u, config.VSphere.InsecureSkipVerify)
		if err != nil {
			return fmt.Errorf("error connecting to vSphere: %w", err)
		}

		// Enable session keep-alive (ping every 5 minutes)
		vsphereClient.RoundTripper = keepalive.NewHandlerSOAP(vsphereClient.RoundTripper, 5*time.Minute, nil)

		provisioner := provider.NewProvisioner(vsphereClient)

		ip, err := infra.NewProvider(meta.ProviderID, provisioner, infra.ProviderConfig{
			Name:        cfg.providerName,
			Description: cfg.providerDescription,
			Icon:        base64.RawStdEncoding.EncodeToString(icon),
			Schema:      schema,
		})
		if err != nil {
			return fmt.Errorf("failed to create infra provider: %w", err)
		}

		logger.Info("starting infra provider")

		clientOptions := []client.Option{
			client.WithInsecureSkipTLSVerify(cfg.insecureSkipVerify),
		}

		if cfg.serviceAccountKey != "" {
			clientOptions = append(clientOptions, client.WithServiceAccount(cfg.serviceAccountKey))
		}

		return ip.Run(
			cmd.Context(),
			logger, infra.WithOmniEndpoint(cfg.omniAPIEndpoint),
			infra.WithClientOptions(
				clientOptions...,
			),
			infra.WithEncodeRequestIDsIntoTokens(),
		)
	},
}

var cfg struct {
	omniAPIEndpoint     string
	serviceAccountKey   string
	providerName        string
	providerDescription string
	configFile          string
	insecureSkipVerify  bool
}

func main() {
	if err := app(); err != nil {
		os.Exit(1)
	}
}

func app() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer cancel()

	return rootCmd.ExecuteContext(ctx)
}

func init() {
	rootCmd.Flags().StringVar(&cfg.omniAPIEndpoint, "omni-api-endpoint", os.Getenv("OMNI_ENDPOINT"),
		"the endpoint of the Omni API, if not set, defaults to OMNI_ENDPOINT env var.")
	rootCmd.Flags().StringVar(&meta.ProviderID, "id", meta.ProviderID, "the id of the infra provider, it is used to match the resources with the infra provider label.")
	rootCmd.Flags().StringVar(&cfg.serviceAccountKey, "omni-service-account-key", os.Getenv("OMNI_SERVICE_ACCOUNT_KEY"), "Omni service account key, if not set, defaults to OMNI_SERVICE_ACCOUNT_KEY.")
	rootCmd.Flags().StringVar(&cfg.providerName, "provider-name", "vsphere", "provider name as it appears in Omni")
	rootCmd.Flags().StringVar(&cfg.providerDescription, "provider-description", "vsphere infrastructure provider", "Provider description as it appears in Omni")
	rootCmd.Flags().BoolVar(&cfg.insecureSkipVerify, "insecure-skip-verify", false, "ignores untrusted certs on Omni side")
	rootCmd.Flags().StringVar(&cfg.configFile, "config-file", "", "vsphere provider config")
}
