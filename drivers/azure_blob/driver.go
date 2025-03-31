package azure_blob

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/model"
)

type AzureBlob struct {
	model.Storage
	Addition
	client          *azblob.Client
	containerClient *container.Client
	config          driver.Config
}

// Config returns driver configuration
func (d *AzureBlob) Config() driver.Config {
	return d.config
}

// GetAddition returns additional driver settings
func (d *AzureBlob) GetAddition() driver.Additional {
	return &d.Addition
}

// Init initializes Azure Blob client
// https://learn.microsoft.com/rest/api/storageservices/authorize-with-shared-key
func (d *AzureBlob) Init(ctx context.Context) error {
	var name = extractAccountName(d.Addition.Endpoint)
	credential, err := azblob.NewSharedKeyCredential(name, d.Addition.AccessKey)
	if err != nil {
		return fmt.Errorf("failed to create credential: %w", err)
	}

	client, err := azblob.NewClientWithSharedKeyCredential(d.Addition.Endpoint, credential,
		&azblob.ClientOptions{ClientOptions: azcore.ClientOptions{
			Retry: policy.RetryOptions{
				MaxRetries: MaxRetries,
				RetryDelay: RetryDelay,
			},
		},
		})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	d.client = client

	var containerName = strings.Trim(d.Addition.GetRootId(), "/\\")
	return d.createContainerIfNotExists(ctx, containerName)
}

// Drop releases resources
func (d *AzureBlob) Drop(ctx context.Context) error {
	d.client = nil
	return nil
}

// List blobs and directories under the given path
// https://learn.microsoft.com/rest/api/storageservices/list-blobs
func (d *AzureBlob) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	prefix := dir.GetPath()
	prefix = ensureTrailingSlash(prefix)

	pager := d.containerClient.NewListBlobsHierarchyPager("/", &container.ListBlobsHierarchyOptions{
		Prefix: &prefix,
	})
	var objs []model.Obj
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list blobs: %w", err)
		}
		for _, blobPrefix := range page.Segment.BlobPrefixes {
			objs = append(objs, &model.Object{
				Name:     path.Base(strings.TrimSuffix(*blobPrefix.Name, "/")),
				Path:     *blobPrefix.Name,
				Modified: *blobPrefix.Properties.LastModified,
				Ctime:    *blobPrefix.Properties.CreationTime,
				IsFolder: true,
			})
		}
		for _, blob := range page.Segment.BlobItems {
			if strings.HasSuffix(*blob.Name, "/") {
				continue
			}
			objs = append(objs, &model.Object{
				Name:     path.Base(*blob.Name),
				Path:     *blob.Name,
				Size:     *blob.Properties.ContentLength,
				Modified: *blob.Properties.LastModified,
				Ctime:    *blob.Properties.CreationTime,
				IsFolder: false,
			})
		}
	}
	return objs, nil
}

// Link generates a temporary SAS URL for blob access
// https://learn.microsoft.com/rest/api/storageservices/create-service-sas
func (d *AzureBlob) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	blobClient := d.containerClient.NewBlobClient(file.GetPath())
	expireDuration := time.Hour * time.Duration(d.SignURLExpire)
	sasURL, err := blobClient.GetSASURL(sas.BlobPermissions{Read: true}, time.Now().Add(expireDuration), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate SAS URL: %w", err)
	}
	return &model.Link{URL: sasURL}, nil
}

// MakeDir 创建一个虚拟目录（通过上传空对象实现）
func (d *AzureBlob) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	dirPath := path.Join(parentDir.GetPath(), dirName)
	err := d.mkDir(ctx, dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create directory marker: %w", err)
	}

	return &model.Object{
		Path:     dirPath,
		Name:     dirName,
		IsFolder: true,
	}, nil
}

// Move moves an object to another directory
func (d *AzureBlob) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	srcPath := srcObj.GetPath()
	dstPath := path.Join(dstDir.GetPath(), srcObj.GetName())

	err := d.moveOrRename(ctx, srcPath, dstPath, srcObj.IsDir(), srcObj.GetSize())
	if err != nil {
		return nil, fmt.Errorf("move operation failed: %w", err)
	}

	return &model.Object{
		Path:     dstPath,
		Name:     srcObj.GetName(),
		Modified: time.Now(),
		IsFolder: srcObj.IsDir(),
		Size:     srcObj.GetSize(),
	}, nil
}

// Rename renames an object
func (d *AzureBlob) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	srcPath := srcObj.GetPath()
	dstPath := path.Join(path.Dir(srcPath), newName)

	err := d.moveOrRename(ctx, srcPath, dstPath, srcObj.IsDir(), srcObj.GetSize())
	if err != nil {
		return nil, fmt.Errorf("rename operation failed: %w", err)
	}

	return &model.Object{
		Path:     dstPath,
		Name:     newName,
		Modified: time.Now(),
		IsFolder: srcObj.IsDir(),
		Size:     srcObj.GetSize(),
	}, nil
}

// Helper function to ensure paths end with slash
func ensureTrailingSlash(path string) string {
	if path != "" && !strings.HasSuffix(path, "/") {
		return path + "/"
	}
	return path
}

// Copy blob to destination directory
// https://learn.microsoft.com/rest/api/storageservices/copy-blob
func (d *AzureBlob) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	dstPath := path.Join(dstDir.GetPath(), srcObj.GetName())

	// Handle directory copying using flat listing
	if srcObj.IsDir() {
		srcPrefix := srcObj.GetPath()
		srcPrefix = ensureTrailingSlash(srcPrefix)

		// Get all blobs under the source directory
		blobs, err := d.flattenListBlobs(ctx, srcPrefix)
		if err != nil {
			return nil, fmt.Errorf("failed to list source directory contents: %w", err)
		}

		// Process each blob - copy to destination
		for _, blob := range blobs {
			// Skip the directory marker itself
			if *blob.Name == srcPrefix {
				continue
			}

			// Calculate relative path from source
			relPath := strings.TrimPrefix(*blob.Name, srcPrefix)
			itemDstPath := path.Join(dstPath, relPath)

			if strings.HasSuffix(itemDstPath, "/") || (blob.Metadata["hdi_isfolder"] != nil && *blob.Metadata["hdi_isfolder"] == "true") {
				// Create directory marker at destination
				err := d.mkDir(ctx, itemDstPath)
				if err != nil {
					return nil, fmt.Errorf("failed to create directory marker [%s]: %w", itemDstPath, err)
				}
			} else {
				// Copy the blob
				if err := d.copyFile(ctx, *blob.Name, itemDstPath); err != nil {
					return nil, fmt.Errorf("failed to copy %s: %w", *blob.Name, err)
				}
			}

		}

		// Create directory marker at destination if needed
		if len(blobs) == 0 {
			err := d.mkDir(ctx, dstPath)
			if err != nil {
				return nil, fmt.Errorf("failed to create directory [%s]: %w", dstPath, err)
			}
		}

		return &model.Object{
			Path:     dstPath,
			Name:     srcObj.GetName(),
			Modified: time.Now(),
			IsFolder: true,
		}, nil
	}

	// Copy a single file
	if err := d.copyFile(ctx, srcObj.GetPath(), dstPath); err != nil {
		return nil, fmt.Errorf("failed to copy blob: %w", err)
	}

	return &model.Object{
		Path:     dstPath,
		Name:     srcObj.GetName(),
		Size:     srcObj.GetSize(),
		Modified: time.Now(),
		IsFolder: false,
	}, nil
}

// Remove deletes the specified blob or recursively deletes a directory
func (d *AzureBlob) Remove(ctx context.Context, obj model.Obj) error {
	path := obj.GetPath()

	// Handle recursive directory deletion
	if obj.IsDir() {
		return d.deleteFolder(ctx, path)
	}

	// Delete single file
	return d.deleteFile(ctx, path, false)
}

// Put uploads a file to Azure Blob Storage
// https://learn.microsoft.com/rest/api/storageservices/put-blob
func (d *AzureBlob) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) (model.Obj, error) {
	blobPath := path.Join(dstDir.GetPath(), stream.GetName())
	blobClient := d.containerClient.NewBlockBlobClient(blobPath)

	// Use optimized upload options based on file size
	options := optimizedUploadOptions(stream.GetSize())

	// Create a reader that tracks upload progress
	progressTracker := &progressTracker{
		total:          stream.GetSize(),
		updateProgress: up,
	}

	// Use LimitedUploadStream to handle context cancellation
	limitedStream := driver.NewLimitedUploadStream(ctx, io.TeeReader(stream, progressTracker))

	_, err := blobClient.UploadStream(ctx, limitedStream, options)
	if err != nil {
		return nil, fmt.Errorf("failed to upload file: %w", err)
	}

	// Return the newly created object
	return &model.Object{
		Path:     blobPath,
		Name:     stream.GetName(),
		Size:     stream.GetSize(),
		Modified: time.Now(),
		IsFolder: false,
	}, nil
}

// // GetArchiveMeta returns archive file meta
// func (d *AzureBlob) GetArchiveMeta(ctx context.Context, obj model.Obj, args model.ArchiveArgs) (model.ArchiveMeta, error) {
// 	return nil, errs.NotImplement
// }

// // ListArchive lists files in archive
// func (d *AzureBlob) ListArchive(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) ([]model.Obj, error) {
// 	return nil, errs.NotImplement
// }

// // Extract extracts file from archive
// func (d *AzureBlob) Extract(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) (*model.Link, error) {
// 	return nil, errs.NotImplement
// }

// // ArchiveDecompress extracts archive to directory
// func (d *AzureBlob) ArchiveDecompress(ctx context.Context, srcObj, dstDir model.Obj, args model.ArchiveDecompressArgs) ([]model.Obj, error) {
// 	return nil, errs.NotImplement
// }

var _ driver.Driver = (*AzureBlob)(nil)
