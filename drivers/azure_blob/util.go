package azure_blob

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/avast/retry-go"
)

const (
	// MaxRetries defines the maximum number of retry attempts for Azure operations
	MaxRetries = 3
	// RetryDelay defines the base delay between retries
	RetryDelay = 1 * time.Second
	// MaxBatchSize defines the maximum number of operations in a single batch request
	MaxBatchSize = 256
)

// isNotFoundError checks if the error is a "not found" type error
func isNotFoundError(err error) bool {
	var storageErr *azcore.ResponseError
	if errors.As(err, &storageErr) {
		return storageErr.StatusCode == 404
	}
	// Fallback to string matching for backwards compatibility
	return err != nil && strings.Contains(err.Error(), "BlobNotFound")
}

// withRetry executes the given operation with retry logic
func (d *AzureBlob) withRetry(operation func() error) error {
	return retry.Do(
		operation,
		retry.Attempts(MaxRetries),
		retry.Delay(RetryDelay),
		retry.DelayType(retry.BackOffDelay),
	)
}

// flattenListBlobs lists all blobs under a prefix using flat listing API
func (d *AzureBlob) flattenListBlobs(ctx context.Context, prefix string) ([]container.BlobItem, error) {
	// Ensure prefix ends with "/" for directory listing
	if !strings.HasSuffix(prefix, "/") && prefix != "" {
		prefix += "/"
	}

	var blobItems []container.BlobItem
	pager := d.containerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
		Prefix: &prefix,
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list blobs: %w", err)
		}

		for _, blob := range page.Segment.BlobItems {
			blobItems = append(blobItems, container.BlobItem(*blob))
		}
	}

	return blobItems, nil
}

// batchDeleteBlobs uses the Azure Blob Batch API to delete multiple blobs in a single request
func (d *AzureBlob) batchDeleteBlobs(ctx context.Context, blobPaths []string) error {
	// Skip for empty list
	if len(blobPaths) == 0 {
		return nil
	}

	// Azure Batch API has a limitation of maximum items per batch request
	for i := 0; i < len(blobPaths); i += MaxBatchSize {
		end := i + MaxBatchSize
		if end > len(blobPaths) {
			end = len(blobPaths)
		}

		currentBatch := blobPaths[i:end]

		// Execute batch deletion with retry
		err := d.withRetry(func() error {
			subBatchSize := 100 // Process in smaller sub-batches for better handling
			for j := 0; j < len(currentBatch); j += subBatchSize {
				subEnd := j + subBatchSize
				if subEnd > len(currentBatch) {
					subEnd = len(currentBatch)
				}

				subBatch := currentBatch[j:subEnd]

				// Create batch builder
				batchBuilder, err := d.containerClient.NewBatchBuilder()
				if err != nil {
					return fmt.Errorf("failed to create batch builder: %w", err)
				}

				// Add delete operations to batch
				for _, blobPath := range subBatch {
					err = batchBuilder.Delete(blobPath, nil)
					if err != nil {
						return fmt.Errorf("failed to add delete operation for %s: %w", blobPath, err)
					}
				}

				// Submit batch
				responses, err := d.containerClient.SubmitBatch(ctx, batchBuilder, nil)
				if err != nil {
					return fmt.Errorf("batch delete request failed: %w", err)
				}

				// Check responses for errors, ignoring "not found" errors
				for _, resp := range responses.Responses {
					if resp.Error != nil && !isNotFoundError(resp.Error) {
						// 获取 blob 名称以提供更好的错误信息
						blobName := "unknown"
						if resp.BlobName != nil {
							blobName = *resp.BlobName
						}
						return fmt.Errorf("failed to delete blob %s: %v", blobName, resp.Error)
					}
				}
			}
			return nil
		})

		if err != nil {
			return err
		}
	}

	return nil
}

// deleteFolder recursively deletes a directory and all its contents using batch API
func (d *AzureBlob) deleteFolder(ctx context.Context, prefix string) error {
	// Get all blobs under the directory using flattenListBlobs
	blobs, err := d.flattenListBlobs(ctx, prefix)
	if err != nil {
		return fmt.Errorf("failed to list blobs for deletion: %w", err)
	}

	// If no blobs found, just try to delete the directory marker
	if len(blobs) == 0 {
		return d.deleteFile(ctx, prefix, true)
	}

	// Extract blob paths for batch deletion
	blobPaths := make([]string, 0, len(blobs))
	for _, blob := range blobs {
		blobPaths = append(blobPaths, *blob.Name)
	}

	// Use batch API to delete all blobs
	if err := d.batchDeleteBlobs(ctx, blobPaths); err != nil {
		return err
	}

	// Also try to delete the directory marker itself
	return d.deleteFile(ctx, prefix, true)
}

// deleteFile deletes a single file or blob with better error handling
func (d *AzureBlob) deleteFile(ctx context.Context, path string, isDir bool) error {
	blobClient := d.containerClient.NewBlobClient(path)

	return d.withRetry(func() error {
		_, err := blobClient.Delete(ctx, nil)

		// Ignore not found errors, especially for directories
		if err != nil && isDir && isNotFoundError(err) {
			return nil
		}
		return err
	})
}

// copyFile copies a single blob from source path to destination path
func (d *AzureBlob) copyFile(ctx context.Context, srcPath, dstPath string) error {
	srcBlob := d.containerClient.NewBlobClient(srcPath)
	dstBlob := d.containerClient.NewBlobClient(dstPath)

	return d.withRetry(func() error {
		// Use configured expiration time for SAS URL
		expireDuration := time.Hour * time.Duration(d.SignURLExpire)
		srcURL, err := srcBlob.GetSASURL(sas.BlobPermissions{Read: true}, time.Now().Add(expireDuration), nil)
		if err != nil {
			return fmt.Errorf("failed to generate source SAS URL: %w", err)
		}

		_, err = dstBlob.StartCopyFromURL(ctx, srcURL, nil)
		return err
	})
}

// optimizedUploadOptions returns the optimal upload options based on file size
func optimizedUploadOptions(fileSize int64) *azblob.UploadStreamOptions {
	options := &azblob.UploadStreamOptions{
		BlockSize:   4 * 1024 * 1024, // 4MB block size
		Concurrency: 4,               // Default concurrency
	}

	// For large files, increase block size and concurrency
	if fileSize > 256*1024*1024 { // For files larger than 256MB
		options.BlockSize = 8 * 1024 * 1024 // 8MB blocks
		options.Concurrency = 8             // More concurrent uploads
	}

	// For very large files (>1GB)
	if fileSize > 1024*1024*1024 {
		options.BlockSize = 16 * 1024 * 1024 // 16MB blocks
		options.Concurrency = 16             // Higher concurrency
	}

	return options
}
