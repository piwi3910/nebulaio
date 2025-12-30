package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"
)

// NewAdminCmd creates the admin command group.
func NewAdminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative operations",
		Long:  `Administrative operations for NebulaIO cluster management.`,
	}

	cmd.AddCommand(newAdminVersioningCmd())
	cmd.AddCommand(newAdminPolicyCmd())
	cmd.AddCommand(newAdminReplicationCmd())
	cmd.AddCommand(newAdminLifecycleCmd())

	return cmd
}

// Versioning commands

func newAdminVersioningCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "versioning",
		Short: "Manage bucket versioning",
	}

	cmd.AddCommand(newVersioningEnableCmd())
	cmd.AddCommand(newVersioningDisableCmd())
	cmd.AddCommand(newVersioningStatusCmd())

	return cmd
}

func newVersioningEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <bucket-name>",
		Short: "Enable versioning for a bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]

			_, err = client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
				Bucket: &bucket,
				VersioningConfiguration: &types.VersioningConfiguration{
					Status: types.BucketVersioningStatusEnabled,
				},
			})
			if err != nil {
				return fmt.Errorf("failed to enable versioning: %w", err)
			}

			fmt.Printf("Versioning enabled for bucket '%s'\n", bucket)

			return nil
		},
	}
}

func newVersioningDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <bucket-name>",
		Short: "Suspend versioning for a bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]

			_, err = client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
				Bucket: &bucket,
				VersioningConfiguration: &types.VersioningConfiguration{
					Status: types.BucketVersioningStatusSuspended,
				},
			})
			if err != nil {
				return fmt.Errorf("failed to suspend versioning: %w", err)
			}

			fmt.Printf("Versioning suspended for bucket '%s'\n", bucket)

			return nil
		},
	}
}

func newVersioningStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <bucket-name>",
		Short: "Get versioning status for a bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]

			result, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
				Bucket: &bucket,
			})
			if err != nil {
				return fmt.Errorf("failed to get versioning status: %w", err)
			}

			status := "Disabled"
			if result.Status != "" {
				status = string(result.Status)
			}

			fmt.Printf("Versioning status for '%s': %s\n", bucket, status)

			return nil
		},
	}
}

// Policy commands

func newAdminPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage bucket policies",
	}

	cmd.AddCommand(newPolicySetCmd())
	cmd.AddCommand(newPolicyGetCmd())
	cmd.AddCommand(newPolicyDeleteCmd())

	return cmd
}

func newPolicySetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <bucket-name> <policy-file>",
		Short: "Set bucket policy from file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]
			policyFile := args[1]

			policyData, err := os.ReadFile(policyFile)
			if err != nil {
				return fmt.Errorf("failed to read policy file: %w", err)
			}

			policy := string(policyData)

			_, err = client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
				Bucket: &bucket,
				Policy: &policy,
			})
			if err != nil {
				return fmt.Errorf("failed to set bucket policy: %w", err)
			}

			fmt.Printf("Policy set for bucket '%s'\n", bucket)

			return nil
		},
	}
}

func newPolicyGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <bucket-name>",
		Short: "Get bucket policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]

			result, err := client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{
				Bucket: &bucket,
			})
			if err != nil {
				return fmt.Errorf("failed to get bucket policy: %w", err)
			}

			if result.Policy != nil {
				// Pretty print JSON
				var prettyJSON map[string]interface{}
				err := json.Unmarshal([]byte(*result.Policy), &prettyJSON)
				if err == nil {
					pretty, _ := json.MarshalIndent(prettyJSON, "", "  ")
					fmt.Println(string(pretty))
				} else {
					fmt.Println(*result.Policy)
				}
			}

			return nil
		},
	}
}

func newPolicyDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <bucket-name>",
		Short: "Delete bucket policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]

			_, err = client.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{
				Bucket: &bucket,
			})
			if err != nil {
				return fmt.Errorf("failed to delete bucket policy: %w", err)
			}

			fmt.Printf("Policy deleted for bucket '%s'\n", bucket)

			return nil
		},
	}
}

// readConfigFile reads and returns the contents of a configuration file.
func readConfigFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	return data, nil
}

// bucketConfigSetterFunc is a function type for setting bucket configurations.
type bucketConfigSetterFunc func(ctx context.Context, client *s3.Client, bucket string, configData []byte) error

// newBucketConfigSetCmd creates a generic bucket config set command.
func newBucketConfigSetCmd(use, short, configType string, setter bucketConfigSetterFunc) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket, configFile := args[0], args[1]
			ctx := context.Background()

			configData, err := readConfigFile(configFile)
			if err != nil {
				return err
			}

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			if err := setter(ctx, client, bucket, configData); err != nil {
				return err
			}

			fmt.Printf("%s configuration set for bucket '%s'\n", configType, bucket)
			return nil
		},
	}
}

// Replication commands

func newAdminReplicationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replication",
		Short: "Manage bucket replication",
	}

	cmd.AddCommand(newReplicationSetCmd())
	cmd.AddCommand(newReplicationGetCmd())
	cmd.AddCommand(newReplicationDeleteCmd())

	return cmd
}

func newReplicationSetCmd() *cobra.Command {
	return newBucketConfigSetCmd(
		"set <bucket-name> <config-file>",
		"Set replication configuration from file",
		"Replication",
		func(ctx context.Context, client *s3.Client, bucket string, configData []byte) error {
			var config types.ReplicationConfiguration
			if err := json.Unmarshal(configData, &config); err != nil {
				return fmt.Errorf("failed to parse replication config: %w", err)
			}
			_, err := client.PutBucketReplication(ctx, &s3.PutBucketReplicationInput{
				Bucket:                   &bucket,
				ReplicationConfiguration: &config,
			})
			if err != nil {
				return fmt.Errorf("failed to set replication: %w", err)
			}
			return nil
		},
	)
}

func newReplicationGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <bucket-name>",
		Short: "Get replication configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]

			result, err := client.GetBucketReplication(ctx, &s3.GetBucketReplicationInput{
				Bucket: &bucket,
			})
			if err != nil {
				return fmt.Errorf("failed to get replication config: %w", err)
			}

			if result.ReplicationConfiguration != nil {
				pretty, _ := json.MarshalIndent(result.ReplicationConfiguration, "", "  ")
				fmt.Println(string(pretty))
			}

			return nil
		},
	}
}

func newReplicationDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <bucket-name>",
		Short: "Delete replication configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]

			_, err = client.DeleteBucketReplication(ctx, &s3.DeleteBucketReplicationInput{
				Bucket: &bucket,
			})
			if err != nil {
				return fmt.Errorf("failed to delete replication config: %w", err)
			}

			fmt.Printf("Replication configuration deleted for bucket '%s'\n", bucket)

			return nil
		},
	}
}

// Lifecycle commands

func newAdminLifecycleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lifecycle",
		Short: "Manage bucket lifecycle rules",
	}

	cmd.AddCommand(newLifecycleSetCmd())
	cmd.AddCommand(newLifecycleGetCmd())
	cmd.AddCommand(newLifecycleDeleteCmd())
	cmd.AddCommand(newLifecycleListCmd())

	return cmd
}

func newLifecycleSetCmd() *cobra.Command {
	return newBucketConfigSetCmd(
		"set <bucket-name> <config-file>",
		"Set lifecycle configuration from file",
		"Lifecycle",
		func(ctx context.Context, client *s3.Client, bucket string, configData []byte) error {
			var lifecycleConfig types.BucketLifecycleConfiguration
			if err := json.Unmarshal(configData, &lifecycleConfig); err != nil {
				return fmt.Errorf("failed to parse lifecycle config: %w", err)
			}
			_, err := client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
				Bucket:                 &bucket,
				LifecycleConfiguration: &lifecycleConfig,
			})
			if err != nil {
				return fmt.Errorf("failed to set lifecycle config: %w", err)
			}
			return nil
		},
	)
}

func newLifecycleGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <bucket-name>",
		Short: "Get lifecycle configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]

			result, err := client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{
				Bucket: &bucket,
			})
			if err != nil {
				return fmt.Errorf("failed to get lifecycle config: %w", err)
			}

			if len(result.Rules) > 0 {
				pretty, _ := json.MarshalIndent(result.Rules, "", "  ")
				fmt.Println(string(pretty))
			} else {
				fmt.Println("No lifecycle rules configured")
			}

			return nil
		},
	}
}

func newLifecycleDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <bucket-name>",
		Short: "Delete lifecycle configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]

			_, err = client.DeleteBucketLifecycle(ctx, &s3.DeleteBucketLifecycleInput{
				Bucket: &bucket,
			})
			if err != nil {
				return fmt.Errorf("failed to delete lifecycle config: %w", err)
			}

			fmt.Printf("Lifecycle configuration deleted for bucket '%s'\n", bucket)

			return nil
		},
	}
}

func newLifecycleListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <bucket-name>",
		Short: "List lifecycle rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucket := args[0]

			result, err := client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{
				Bucket: &bucket,
			})
			if err != nil {
				return fmt.Errorf("failed to get lifecycle config: %w", err)
			}

			if len(result.Rules) == 0 {
				fmt.Println("No lifecycle rules configured")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tSTATUS\tPREFIX\tEXPIRATION")

			for _, rule := range result.Rules {
				id := ""
				if rule.ID != nil {
					id = *rule.ID
				}

				status := string(rule.Status)

				prefix := ""
				if rule.Filter != nil && rule.Filter.Prefix != nil {
					prefix = *rule.Filter.Prefix
				}

				expiration := ""

				if rule.Expiration != nil {
					if rule.Expiration.Days != nil {
						expiration = fmt.Sprintf("%d days", *rule.Expiration.Days)
					} else if rule.Expiration.Date != nil {
						expiration = rule.Expiration.Date.Format("2006-01-02")
					}
				}

				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, status, prefix, expiration)
			}

			return w.Flush()
		},
	}
}
