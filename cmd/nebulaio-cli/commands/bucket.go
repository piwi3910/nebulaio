package commands

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
)

// NewBucketCmd creates the bucket command group.
func NewBucketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bucket",
		Short: "Bucket operations",
		Long:  `Manage buckets in NebulaIO.`,
	}

	cmd.AddCommand(newBucketCreateCmd())
	cmd.AddCommand(newBucketDeleteCmd())
	cmd.AddCommand(newBucketListCmd())
	cmd.AddCommand(newBucketInfoCmd())

	return cmd
}

func newBucketCreateCmd() *cobra.Command {
	var (
		region     string
		objectLock bool
	)

	cmd := &cobra.Command{
		Use:   "create <bucket-name>",
		Short: "Create a new bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucketName := args[0]

			input := &s3.CreateBucketInput{
				Bucket: &bucketName,
			}

			if objectLock {
				input.ObjectLockEnabledForBucket = &objectLock
			}

			_, err = client.CreateBucket(ctx, input)
			if err != nil {
				return fmt.Errorf("failed to create bucket: %w", err)
			}

			fmt.Printf("Bucket '%s' created successfully\n", bucketName)

			return nil
		},
	}

	cmd.Flags().StringVar(&region, "region", "", "Bucket region")
	cmd.Flags().BoolVar(&objectLock, "object-lock", false, "Enable object lock")

	return cmd
}

func newBucketDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <bucket-name>",
		Short: "Delete a bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucketName := args[0]

			if force {
				// Delete all objects first
				err := deleteAllObjects(ctx, client, bucketName)
				if err != nil {
					return fmt.Errorf("failed to delete objects: %w", err)
				}
			}

			_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
				Bucket: &bucketName,
			})
			if err != nil {
				return fmt.Errorf("failed to delete bucket: %w", err)
			}

			fmt.Printf("Bucket '%s' deleted successfully\n", bucketName)

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Delete all objects before deleting bucket")

	return cmd
}

func newBucketListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List all buckets",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
			if err != nil {
				return fmt.Errorf("failed to list buckets: %w", err)
			}

			if len(result.Buckets) == 0 {
				fmt.Println("No buckets found")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

			_, _ = fmt.Fprintln(w, "NAME\tCREATED")
			for _, bucket := range result.Buckets {
				created := ""
				if bucket.CreationDate != nil {
					created = bucket.CreationDate.Format(time.RFC3339)
				}

				_, _ = fmt.Fprintf(w, "%s\t%s\n", *bucket.Name, created)
			}

			return w.Flush()
		},
	}
}

func newBucketInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <bucket-name>",
		Short: "Get bucket information",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			bucketName := args[0]

			// Get versioning
			versioning, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
				Bucket: &bucketName,
			})
			if err != nil {
				fmt.Printf("Versioning: unknown\n")
			} else {
				status := "Disabled"
				if versioning.Status != "" {
					status = string(versioning.Status)
				}

				fmt.Printf("Versioning: %s\n", status)
			}

			// Get location
			location, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
				Bucket: &bucketName,
			})
			if err == nil && location.LocationConstraint != "" {
				fmt.Printf("Region: %s\n", location.LocationConstraint)
			}

			// Get object count and total size
			var (
				objectCount int64
				totalSize   int64
			)

			paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
				Bucket: &bucketName,
			})

			var paginationErr error

			for paginator.HasMorePages() {
				page, err := paginator.NextPage(ctx)
				if err != nil {
					paginationErr = err

					break
				}

				objectCount += int64(len(page.Contents))
				for _, obj := range page.Contents {
					if obj.Size != nil {
						totalSize += *obj.Size
					}
				}
			}

			fmt.Printf("Objects: %d\n", objectCount)
			fmt.Printf("Total Size: %s\n", FormatSize(totalSize))

			return paginationErr
		},
	}
}

// NewMakeBucketCmd creates the mb alias command.
func NewMakeBucketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mb <s3://bucket-name>",
		Short: "Make a bucket (alias for 'bucket create')",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket, _, isS3 := ParseS3URI(args[0])
			if !isS3 {
				bucket = args[0]
			}

			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
				Bucket: &bucket,
			})
			if err != nil {
				return fmt.Errorf("failed to create bucket: %w", err)
			}

			fmt.Printf("Bucket '%s' created successfully\n", bucket)

			return nil
		},
	}

	return cmd
}

// NewRemoveBucketCmd creates the rb alias command.
func NewRemoveBucketCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "rb <s3://bucket-name>",
		Short: "Remove a bucket (alias for 'bucket delete')",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket, _, isS3 := ParseS3URI(args[0])
			if !isS3 {
				bucket = args[0]
			}

			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			if force {
				err := deleteAllObjects(ctx, client, bucket)
				if err != nil {
					return fmt.Errorf("failed to delete objects: %w", err)
				}
			}

			_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{
				Bucket: &bucket,
			})
			if err != nil {
				return fmt.Errorf("failed to delete bucket: %w", err)
			}

			fmt.Printf("Bucket '%s' deleted successfully\n", bucket)

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Delete all objects before deleting bucket")

	return cmd
}

// deleteAllObjects deletes all objects in a bucket.
func deleteAllObjects(ctx context.Context, client *s3.Client, bucket string) error {
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: &bucket,
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}

		for _, obj := range page.Contents {
			_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: &bucket,
				Key:    obj.Key,
			})
			if err != nil {
				return fmt.Errorf("failed to delete %s: %w", *obj.Key, err)
			}
		}
	}

	return nil
}
