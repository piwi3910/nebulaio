package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// NewConfigCmd creates the config command
func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
		Long:  `Configure the NebulaIO CLI with endpoint and credentials.`,
	}

	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigShowCmd())

	return cmd
}

func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value. Available keys:
  endpoint    - The NebulaIO/S3 endpoint URL
  access-key  - The access key ID
  secret-key  - The secret access key
  region      - The AWS region (default: us-east-1)`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.ToLower(args[0])
			value := args[1]

			cfg, err := LoadConfig()
			if err != nil {
				cfg = DefaultConfig()
			}

			switch key {
			case "endpoint":
				cfg.Endpoint = value
			case "access-key", "accesskey":
				cfg.AccessKey = value
			case "secret-key", "secretkey":
				cfg.SecretKey = value
			case "region":
				cfg.Region = value
			case "use-ssl", "usessl":
				cfg.UseSSL = value == "true" || value == "1" || value == "yes"
			case "skip-verify", "skipverify":
				cfg.SkipVerify = value == "true" || value == "1" || value == "yes"
			default:
				return fmt.Errorf("unknown configuration key: %s", key)
			}

			if err := SaveConfig(cfg); err != nil {
				return err
			}

			fmt.Printf("Set %s = %s\n", key, maskSecret(key, value))
			return nil
		},
	}
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.ToLower(args[0])

			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			var value string
			switch key {
			case "endpoint":
				value = cfg.Endpoint
			case "access-key", "accesskey":
				value = cfg.AccessKey
			case "secret-key", "secretkey":
				value = maskSecret(key, cfg.SecretKey)
			case "region":
				value = cfg.Region
			case "use-ssl", "usessl":
				value = fmt.Sprintf("%t", cfg.UseSSL)
			case "skip-verify", "skipverify":
				value = fmt.Sprintf("%t", cfg.SkipVerify)
			default:
				return fmt.Errorf("unknown configuration key: %s", key)
			}

			fmt.Println(value)
			return nil
		},
	}
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show all configuration values",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig()
			if err != nil {
				return err
			}

			fmt.Printf("endpoint:    %s\n", cfg.Endpoint)
			fmt.Printf("access-key:  %s\n", maskSecret("access-key", cfg.AccessKey))
			fmt.Printf("secret-key:  %s\n", maskSecret("secret-key", cfg.SecretKey))
			fmt.Printf("region:      %s\n", cfg.Region)
			fmt.Printf("use-ssl:     %t\n", cfg.UseSSL)
			fmt.Printf("skip-verify: %t\n", cfg.SkipVerify)
			return nil
		},
	}
}

// maskSecret masks a secret value, showing only first and last 4 chars
func maskSecret(key, value string) string {
	if !strings.Contains(key, "secret") && !strings.Contains(key, "key") {
		return value
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}
