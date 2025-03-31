package azure_blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
)

const (
	// MaxRetries defines the maximum number of retry attempts for Azure operations
	MaxRetries = 3
	// RetryDelay defines the base delay between retries
	RetryDelay = 1 * time.Second
	// MaxBatchSize defines the maximum number of operations in a single batch request
	MaxBatchSize = 256
)

// extractAccountName 从 Azure 存储 Endpoint 中提取账户名
func extractAccountName(endpoint string) string {
	// 移除协议前缀
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")

	// 获取第一个点之前的部分（即账户名）
	parts := strings.Split(endpoint, ".")
	if len(parts) > 0 {
		// to lower case
		return strings.ToLower(parts[0])
	}
	return ""
}

// isNotFoundError checks if the error is a "not found" type error
func isNotFoundError(err error) bool {
	var storageErr *azcore.ResponseError
	if errors.As(err, &storageErr) {
		return storageErr.StatusCode == 404
	}
	// Fallback to string matching for backwards compatibility
	return err != nil && strings.Contains(err.Error(), "BlobNotFound")
}

// flattenListBlobs - Optimize blob listing to handle pagination better
func (d *AzureBlob) flattenListBlobs(ctx context.Context, prefix string) ([]container.BlobItem, error) {
	// Standardize prefix format
	prefix = ensureTrailingSlash(prefix)

	var blobItems []container.BlobItem
	pager := d.containerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
		Prefix: &prefix,
		Include: container.ListBlobsInclude{
			Metadata: true,
		},
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list blobs: %w", err)
		}

		for _, blob := range page.Segment.BlobItems {
			blobItems = append(blobItems, *blob)
		}
	}

	return blobItems, nil
}

// batchDeleteBlobs - Simplify batch deletion logic
func (d *AzureBlob) batchDeleteBlobs(ctx context.Context, blobPaths []string) error {
	if len(blobPaths) == 0 {
		return nil
	}

	// Process in batches of MaxBatchSize
	for i := 0; i < len(blobPaths); i += MaxBatchSize {
		end := min(i+MaxBatchSize, len(blobPaths))
		currentBatch := blobPaths[i:end]

		// Create batch builder
		batchBuilder, err := d.containerClient.NewBatchBuilder()
		if err != nil {
			return fmt.Errorf("failed to create batch builder: %w", err)
		}

		// Add delete operations
		for _, blobPath := range currentBatch {
			if err := batchBuilder.Delete(blobPath, nil); err != nil {
				return fmt.Errorf("failed to add delete operation for %s: %w", blobPath, err)
			}
		}

		// Submit batch
		responses, err := d.containerClient.SubmitBatch(ctx, batchBuilder, nil)
		if err != nil {
			return fmt.Errorf("batch delete request failed: %w", err)
		}

		// Check responses
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
}

// deleteFolder recursively deletes a directory and all its contents
func (d *AzureBlob) deleteFolder(ctx context.Context, prefix string) error {
	// Ensure directory path ends with slash
	prefix = ensureTrailingSlash(prefix)

	// Get all blobs under the directory using flattenListBlobs
	blobs, err := d.flattenListBlobs(ctx, prefix)
	if err != nil {
		return fmt.Errorf("failed to list blobs for deletion: %w", err)
	}

	// If there are blobs in the directory, delete them
	if len(blobs) > 0 {
		// 分离文件和目录标记
		var filePaths []string
		var dirPaths []string

		for _, blob := range blobs {
			blobName := *blob.Name
			if isDirectory(blob) {
				dirPaths = append(dirPaths, blobName)
			} else {
				filePaths = append(filePaths, blobName)
			}
		}

		// 先删除文件，再删除目录
		if len(filePaths) > 0 {
			// Extract blob paths for batch deletion
			if err := d.batchDeleteBlobs(ctx, filePaths); err != nil {
				return err
			}
		}
		// Delete directory markers
		if len(dirPaths) > 0 {
			if err := d.batchDeleteBlobs(ctx, dirPaths); err != nil {
				return err
			}
		}

		// Always try to delete the directory marker itself
		// This is important even if no blobs were found
		path := strings.TrimSuffix(prefix, "/")
		blobClient := d.containerClient.NewBlobClient(path)
		_, err = blobClient.Delete(ctx, nil)

		// Ignore not found errors for directories
		var responseErr *azcore.ResponseError
		if err != nil && errors.As(err, &responseErr) && responseErr.StatusCode == 404 {
			return nil // Directory marker might not exist, which is OK
		}

		return err
	}

	// Always try to delete the directory marker itself
	// This is important even if no blobs were found
	path := strings.TrimSuffix(prefix, "/")
	blobClient := d.containerClient.NewBlobClient(path)
	_, err = blobClient.Delete(ctx, nil)

	// Ignore not found errors for directories
	var responseErr *azcore.ResponseError
	if err != nil && errors.As(err, &responseErr) && responseErr.StatusCode == 404 {
		return nil // Directory marker might not exist, which is OK
	}

	return err
}

// deleteFile deletes a single file or blob with better error handling
func (d *AzureBlob) deleteFile(ctx context.Context, path string, isDir bool) error {
	blobClient := d.containerClient.NewBlobClient(path)

	_, err := blobClient.Delete(ctx, nil)
	// Ignore not found errors, especially for directories
	if err != nil && isDir && isNotFoundError(err) {
		return nil
	}
	return err
}

// copyFile copies a single blob from source path to destination path
func (d *AzureBlob) copyFile(ctx context.Context, srcPath, dstPath string) error {
	srcBlob := d.containerClient.NewBlobClient(srcPath)
	dstBlob := d.containerClient.NewBlobClient(dstPath)

	// Use configured expiration time for SAS URL
	expireDuration := time.Hour * time.Duration(d.SignURLExpire)
	srcURL, err := srcBlob.GetSASURL(sas.BlobPermissions{Read: true}, time.Now().Add(expireDuration), nil)
	if err != nil {
		return fmt.Errorf("failed to generate source SAS URL: %w", err)
	}

	_, err = dstBlob.StartCopyFromURL(ctx, srcURL, nil)
	return err

}

// createContainerIfNotExists - Create container if not exists
// Clean up commented code
func (d *AzureBlob) createContainerIfNotExists(ctx context.Context, containerName string) error {
	serviceClient := d.client.ServiceClient()
	containerClient := serviceClient.NewContainerClient(containerName)

	var options = service.CreateContainerOptions{}
	_, err := containerClient.Create(ctx, &options)
	if err != nil {
		var responseErr *azcore.ResponseError
		if errors.As(err, &responseErr) && responseErr.ErrorCode != "ContainerAlreadyExists" {
			return fmt.Errorf("failed to create or access container [%s]: %w", containerName, err)
		}
	}

	d.containerClient = containerClient
	return nil
}

func (d *AzureBlob) mkDir(ctx context.Context, fullDirName string) error {
	dirPath := ensureTrailingSlash(fullDirName)
	blobClient := d.containerClient.NewBlockBlobClient(dirPath)

	// upload an empty blob to create the directory marker
	_, err := blobClient.Upload(ctx, struct {
		*bytes.Reader
		io.Closer
	}{Reader: bytes.NewReader([]byte{}), Closer: io.NopCloser(nil)}, &blockblob.UploadOptions{
		Metadata: map[string]*string{
			"hdi_isfolder": to.Ptr("true"),
		},
	})
	return err
}

// Helper function for moving or renaming objects
func (d *AzureBlob) moveOrRename(ctx context.Context, srcPath, dstPath string, isDir bool, srcSize int64) error {
	if isDir {
		// Normalize paths for directory operations
		srcPath = ensureTrailingSlash(srcPath)
		dstPath = ensureTrailingSlash(dstPath)

		// List source directory content
		blobs, err := d.flattenListBlobs(ctx, srcPath)
		if err != nil {
			return fmt.Errorf("failed to list blobs: %w", err)
		}

		// Process each blob
		for _, item := range blobs {
			srcBlobName := *item.Name
			relPath := strings.TrimPrefix(srcBlobName, srcPath)
			itemDstPath := path.Join(dstPath, relPath)

			if isDirectory(item) {
				// Create directory marker at destination
				err := d.mkDir(ctx, itemDstPath)
				if err != nil {
					return fmt.Errorf("failed to create directory marker [%s]: %w", itemDstPath, err)
				}
			} else {
				if err := d.copyFile(ctx, srcBlobName, itemDstPath); err != nil {
					return fmt.Errorf("failed to copy blob %s: %w", srcBlobName, err)
				}
			}
		}

		// Create directory marker in destination if no blobs were found
		// This handles empty directory cases
		if len(blobs) == 0 {
			err := d.mkDir(ctx, dstPath)
			if err != nil {
				return fmt.Errorf("failed to create directory [%s]: %w", dstPath, err)
			}
		}

		// Delete source directory and its contents
		return d.deleteFolder(ctx, srcPath)
	}

	// For single file
	if err := d.copyFile(ctx, srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return d.deleteFile(ctx, srcPath, false)
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

// isDirectory determines if a blob represents a directory
// Checks multiple indicators: path suffix, metadata, and content type
func isDirectory(blob container.BlobItem) bool {
	// Check path suffix
	if strings.HasSuffix(*blob.Name, "/") {
		return true
	}

	// Check metadata for directory marker
	if blob.Metadata != nil {
		if val, ok := blob.Metadata["hdi_isfolder"]; ok && val != nil && *val == "true" {
			return true
		}
		// Azure Storage Explorer 和其他工具可能使用不同的元数据键
		if val, ok := blob.Metadata["is_directory"]; ok && val != nil && strings.ToLower(*val) == "true" {
			return true
		}
	}

	// Check content type (some tools mark directories with specific content types)
	if blob.Properties != nil && blob.Properties.ContentType != nil {
		contentType := strings.ToLower(*blob.Properties.ContentType)
		if blob.Properties.ContentLength != nil && *blob.Properties.ContentLength == 0 && (contentType == "application/directory" || contentType == "directory") {
			return true
		}
	}

	return false
}
