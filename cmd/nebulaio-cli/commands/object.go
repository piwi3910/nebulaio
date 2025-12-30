package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
)

// NewObjectCmd creates the object command group.
func NewObjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "object",
		Short: "Object operations",
		Long:  `Manage objects in NebulaIO buckets.`,
	}

	cmd.AddCommand(newObjectPutCmd())
	cmd.AddCommand(newObjectGetCmd())
	cmd.AddCommand(newObjectDeleteCmd())
	cmd.AddCommand(newObjectListCmd())
	cmd.AddCommand(newObjectHeadCmd())

	return cmd
}

func newObjectPutCmd() *cobra.Command {
	var (
		contentType string
		metadata    []string
	)

	cmd := &cobra.Command{
		Use:   "put <local-file> <s3://bucket/key>",
		Short: "Upload an object",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			localFile := args[0]

			bucket, key, isS3 := ParseS3URI(args[1])
			if !isS3 {
				return fmt.Errorf("invalid S3 URI: %s", args[1])
			}

			// If key is empty, use filename
			if key == "" {
				key = filepath.Base(localFile)
			}

			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			file, err := os.Open(localFile)
			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}

			defer func() { _ = file.Close() }()

			stat, err := file.Stat()
			if err != nil {
				return fmt.Errorf("failed to stat file: %w", err)
			}

			input := &s3.PutObjectInput{
				Bucket:        &bucket,
				Key:           &key,
				Body:          file,
				ContentLength: aws.Int64(stat.Size()),
			}

			if contentType != "" {
				input.ContentType = &contentType
			}

			// Parse metadata
			if len(metadata) > 0 {
				input.Metadata = make(map[string]string)

				for _, m := range metadata {
					parts := strings.SplitN(m, "=", 2)
					if len(parts) == 2 {
						input.Metadata[parts[0]] = parts[1]
					}
				}
			}

			result, err := client.PutObject(ctx, input)
			if err != nil {
				return fmt.Errorf("failed to upload object: %w", err)
			}

			fmt.Printf("Uploaded %s to s3://%s/%s\n", localFile, bucket, key)

			if result.ETag != nil {
				fmt.Printf("ETag: %s\n", *result.ETag)
			}

			if result.VersionId != nil {
				fmt.Printf("VersionId: %s\n", *result.VersionId)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&contentType, "content-type", "", "Content type of the object")
	cmd.Flags().StringArrayVar(&metadata, "metadata", nil, "Metadata key=value pairs")

	return cmd
}

func newObjectGetCmd() *cobra.Command {
	var versionID string

	cmd := &cobra.Command{
		Use:   "get <s3://bucket/key> [local-file]",
		Short: "Download an object",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket, key, isS3 := ParseS3URI(args[0])
			if !isS3 || key == "" {
				return fmt.Errorf("invalid S3 URI: %s", args[0])
			}

			localFile := filepath.Base(key)
			if len(args) > 1 {
				localFile = args[1]
			}

			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			input := &s3.GetObjectInput{
				Bucket: &bucket,
				Key:    &key,
			}
			if versionID != "" {
				input.VersionId = &versionID
			}

			result, err := client.GetObject(ctx, input)
			if err != nil {
				return fmt.Errorf("failed to get object: %w", err)
			}

			defer func() { _ = result.Body.Close() }()

			file, err := os.Create(localFile)
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			defer func() { _ = file.Close() }()

			n, err := io.Copy(file, result.Body)
			if err != nil {
				return fmt.Errorf("failed to write file: %w", err)
			}

			fmt.Printf("Downloaded s3://%s/%s to %s (%s)\n", bucket, key, localFile, FormatSize(n))

			return nil
		},
	}

	cmd.Flags().StringVar(&versionID, "version-id", "", "Version ID of the object")

	return cmd
}

func newObjectDeleteCmd() *cobra.Command {
	var (
		versionID string
		recursive bool
	)

	cmd := &cobra.Command{
		Use:   "delete <s3://bucket/key>",
		Short: "Delete an object",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket, key, isS3 := ParseS3URI(args[0])
			if !isS3 {
				return fmt.Errorf("invalid S3 URI: %s", args[0])
			}

			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			if recursive && key != "" {
				// Delete all objects with prefix
				return deleteObjectsWithPrefix(ctx, client, bucket, key)
			}

			if key == "" {
				return errors.New("key is required (use --recursive for prefix deletion)")
			}

			input := &s3.DeleteObjectInput{
				Bucket: &bucket,
				Key:    &key,
			}
			if versionID != "" {
				input.VersionId = &versionID
			}

			_, err = client.DeleteObject(ctx, input)
			if err != nil {
				return fmt.Errorf("failed to delete object: %w", err)
			}

			fmt.Printf("Deleted s3://%s/%s\n", bucket, key)

			return nil
		},
	}

	cmd.Flags().StringVar(&versionID, "version-id", "", "Version ID of the object")
	cmd.Flags().BoolVar(&recursive, "recursive", false, "Delete all objects with prefix")

	return cmd
}

func newObjectListCmd() *cobra.Command {
	var (
		recursive bool
		maxKeys   int
	)

	cmd := &cobra.Command{
		Use:   "list <s3://bucket[/prefix]>",
		Short: "List objects in a bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket, prefix, isS3 := ParseS3URI(args[0])
			if !isS3 {
				return fmt.Errorf("invalid S3 URI: %s", args[0])
			}

			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			//nolint:gosec // G115: maxKeys is validated to be within int32 range (max 1000)
			input := &s3.ListObjectsV2Input{
				Bucket:  &bucket,
				MaxKeys: aws.Int32(int32(maxKeys)),
			}
			if prefix != "" {
				input.Prefix = &prefix
			}

			if !recursive {
				delimiter := "/"
				input.Delimiter = &delimiter
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "KEY\tSIZE\tMODIFIED")

			paginator := s3.NewListObjectsV2Paginator(client, input)
			for paginator.HasMorePages() {
				page, err := paginator.NextPage(ctx)
				if err != nil {
					return fmt.Errorf("failed to list objects: %w", err)
				}

				// Print common prefixes (directories)
				for _, prefix := range page.CommonPrefixes {
					if prefix.Prefix != nil {
						_, _ = fmt.Fprintf(w, "%s\t-\t-\n", *prefix.Prefix)
					}
				}

				// Print objects
				for _, obj := range page.Contents {
					modified := ""
					if obj.LastModified != nil {
						modified = obj.LastModified.Format(time.RFC3339)
					}

					size := int64(0)
					if obj.Size != nil {
						size = *obj.Size
					}

					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", *obj.Key, FormatSize(size), modified)
				}
			}

			return w.Flush()
		},
	}

	cmd.Flags().BoolVar(&recursive, "recursive", false, "List recursively")
	cmd.Flags().IntVar(&maxKeys, "max-keys", 1000, "Maximum number of keys to return")

	return cmd
}

func newObjectHeadCmd() *cobra.Command {
	var versionID string

	cmd := &cobra.Command{
		Use:   "head <s3://bucket/key>",
		Short: "Get object metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket, key, isS3 := ParseS3URI(args[0])
			if !isS3 || key == "" {
				return fmt.Errorf("invalid S3 URI: %s", args[0])
			}

			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			input := &s3.HeadObjectInput{
				Bucket: &bucket,
				Key:    &key,
			}
			if versionID != "" {
				input.VersionId = &versionID
			}

			result, err := client.HeadObject(ctx, input)
			if err != nil {
				return fmt.Errorf("failed to get object metadata: %w", err)
			}

			fmt.Printf("Key: %s\n", key)

			if result.ContentLength != nil {
				fmt.Printf("Size: %s (%d bytes)\n", FormatSize(*result.ContentLength), *result.ContentLength)
			}

			if result.ContentType != nil {
				fmt.Printf("Content-Type: %s\n", *result.ContentType)
			}

			if result.ETag != nil {
				fmt.Printf("ETag: %s\n", *result.ETag)
			}

			if result.LastModified != nil {
				fmt.Printf("Last-Modified: %s\n", result.LastModified.Format(time.RFC3339))
			}

			if result.VersionId != nil {
				fmt.Printf("Version-Id: %s\n", *result.VersionId)
			}

			if result.StorageClass != "" {
				fmt.Printf("Storage-Class: %s\n", result.StorageClass)
			}

			if len(result.Metadata) > 0 {
				fmt.Println("Metadata:")

				for k, v := range result.Metadata {
					fmt.Printf("  %s: %s\n", k, v)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&versionID, "version-id", "", "Version ID of the object")

	return cmd
}

// NewListCmd creates the ls alias command.
func NewListCmd() *cobra.Command {
	var recursive bool

	cmd := &cobra.Command{
		Use:   "ls [s3://bucket[/prefix]]",
		Short: "List buckets or objects",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			// No args - list buckets
			if len(args) == 0 {
				result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
				if err != nil {
					return fmt.Errorf("failed to list buckets: %w", err)
				}

				for _, bucket := range result.Buckets {
					created := ""
					if bucket.CreationDate != nil {
						created = bucket.CreationDate.Format("2006-01-02 15:04:05")
					}

					fmt.Printf("%s  s3://%s\n", created, *bucket.Name)
				}

				return nil
			}

			// List objects
			bucket, prefix, isS3 := ParseS3URI(args[0])
			if !isS3 {
				bucket = args[0]
			}

			input := &s3.ListObjectsV2Input{
				Bucket: &bucket,
			}
			if prefix != "" {
				input.Prefix = &prefix
			}

			if !recursive {
				delimiter := "/"
				input.Delimiter = &delimiter
			}

			paginator := s3.NewListObjectsV2Paginator(client, input)
			for paginator.HasMorePages() {
				page, err := paginator.NextPage(ctx)
				if err != nil {
					return fmt.Errorf("failed to list objects: %w", err)
				}

				// Print common prefixes (directories)
				for _, p := range page.CommonPrefixes {
					if p.Prefix != nil {
						fmt.Printf("                           PRE %s\n", *p.Prefix)
					}
				}

				// Print objects
				for _, obj := range page.Contents {
					modified := ""
					if obj.LastModified != nil {
						modified = obj.LastModified.Format("2006-01-02 15:04:05")
					}

					size := int64(0)
					if obj.Size != nil {
						size = *obj.Size
					}

					fmt.Printf("%s %10d %s\n", modified, size, *obj.Key)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "List recursively")

	return cmd
}

// NewCopyCmd creates the cp alias command.
func NewCopyCmd() *cobra.Command {
	var recursive bool

	cmd := &cobra.Command{
		Use:   "cp <source> <destination>",
		Short: "Copy files to/from S3",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := args[0]
			dst := args[1]

			srcBucket, srcKey, srcIsS3 := ParseS3URI(src)
			dstBucket, dstKey, dstIsS3 := ParseS3URI(dst)

			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			switch {
			case !srcIsS3 && dstIsS3:
				// Upload: local -> S3
				return uploadFile(ctx, client, src, dstBucket, dstKey, recursive)
			case srcIsS3 && !dstIsS3:
				// Download: S3 -> local
				return downloadFile(ctx, client, srcBucket, srcKey, dst, recursive)
			case srcIsS3 && dstIsS3:
				// Copy: S3 -> S3
				return copyS3Object(ctx, client, srcBucket, srcKey, dstBucket, dstKey)
			default:
				return errors.New("at least one of source or destination must be an S3 URI")
			}
		},
	}

	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Copy recursively")

	return cmd
}

// NewRemoveCmd creates the rm alias command.
func NewRemoveCmd() *cobra.Command {
	var recursive bool

	cmd := &cobra.Command{
		Use:   "rm <s3://bucket/key>",
		Short: "Remove objects from S3",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket, key, isS3 := ParseS3URI(args[0])
			if !isS3 {
				return fmt.Errorf("invalid S3 URI: %s", args[0])
			}

			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			if recursive {
				return deleteObjectsWithPrefix(ctx, client, bucket, key)
			}

			if key == "" {
				return errors.New("key is required (use --recursive for prefix deletion)")
			}

			_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: &bucket,
				Key:    &key,
			})
			if err != nil {
				return fmt.Errorf("failed to delete object: %w", err)
			}

			fmt.Printf("delete: s3://%s/%s\n", bucket, key)

			return nil
		},
	}

	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Remove all objects with prefix")

	return cmd
}

// NewCatCmd creates the cat command.
func NewCatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cat <s3://bucket/key>",
		Short: "Display object contents",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bucket, key, isS3 := ParseS3URI(args[0])
			if !isS3 || key == "" {
				return fmt.Errorf("invalid S3 URI: %s", args[0])
			}

			ctx := context.Background()

			client, err := NewS3Client(ctx)
			if err != nil {
				return err
			}

			result, err := client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: &bucket,
				Key:    &key,
			})
			if err != nil {
				return fmt.Errorf("failed to get object: %w", err)
			}

			defer func() { _ = result.Body.Close() }()

			_, err = io.Copy(os.Stdout, result.Body)

			return err
		},
	}
}

// Helper functions

func uploadFile(ctx context.Context, client *s3.Client, localPath, bucket, key string, recursive bool) error {
	stat, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.IsDir() && recursive {
		return filepath.Walk(localPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}

			relPath, _ := filepath.Rel(localPath, path)

			objKey := key
			if objKey != "" && !strings.HasSuffix(objKey, "/") {
				objKey += "/"
			}

			objKey += relPath

			return uploadSingleFile(ctx, client, path, bucket, objKey)
		})
	}

	if key == "" {
		key = filepath.Base(localPath)
	}

	return uploadSingleFile(ctx, client, localPath, bucket, key)
}

func uploadSingleFile(ctx context.Context, client *s3.Client, localPath, bucket, key string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &bucket,
		Key:           &key,
		Body:          file,
		ContentLength: aws.Int64(stat.Size()),
	})
	if err != nil {
		return fmt.Errorf("failed to upload %s: %w", localPath, err)
	}

	fmt.Printf("upload: %s to s3://%s/%s\n", localPath, bucket, key)

	return nil
}

func downloadFile(ctx context.Context, client *s3.Client, bucket, key, localPath string, recursive bool) error {
	if recursive && (key == "" || strings.HasSuffix(key, "/")) {
		// Download all objects with prefix
		paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
			Bucket: &bucket,
			Prefix: &key,
		})

		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return err
			}

			for _, obj := range page.Contents {
				relPath := strings.TrimPrefix(*obj.Key, key)
				destPath := filepath.Join(localPath, relPath)

				err := downloadSingleFile(ctx, client, bucket, *obj.Key, destPath)
				if err != nil {
					return err
				}
			}
		}

		return nil
	}

	return downloadSingleFile(ctx, client, bucket, key, localPath)
}

func downloadSingleFile(ctx context.Context, client *s3.Client, bucket, key, localPath string) error {
	result, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("failed to get object: %w", err)
	}

	defer func() { _ = result.Body.Close() }()

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}

	file, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	defer func() { _ = file.Close() }()

	if _, err := io.Copy(file, result.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("download: s3://%s/%s to %s\n", bucket, key, localPath)

	return nil
}

func copyS3Object(ctx context.Context, client *s3.Client, srcBucket, srcKey, dstBucket, dstKey string) error {
	copySource := srcBucket + "/" + srcKey

	if dstKey == "" {
		dstKey = srcKey
	}

	_, err := client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     &dstBucket,
		Key:        &dstKey,
		CopySource: &copySource,
	})
	if err != nil {
		return fmt.Errorf("failed to copy object: %w", err)
	}

	fmt.Printf("copy: s3://%s/%s to s3://%s/%s\n", srcBucket, srcKey, dstBucket, dstKey)

	return nil
}

func deleteObjectsWithPrefix(ctx context.Context, client *s3.Client, bucket, prefix string) error {
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: &bucket,
		Prefix: &prefix,
	})

	count := 0

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

			fmt.Printf("delete: s3://%s/%s\n", bucket, *obj.Key)

			count++
		}
	}

	fmt.Printf("Deleted %d objects\n", count)

	return nil
}
