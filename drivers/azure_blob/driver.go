package azure_blob

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
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
	credential, err := azblob.NewSharedKeyCredential(d.Addition.Name, d.Addition.Key)
	if err != nil {
		return fmt.Errorf("failed to create credential: %w", err)
	}
	client, err := azblob.NewClientWithSharedKeyCredential(d.Addition.Endpoint, credential, nil)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	d.client = client
	d.containerClient = client.ServiceClient().NewContainerClient(d.Addition.Container)
	return nil
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
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
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
				Name:     strings.TrimSuffix(*blobPrefix.Name, "/"),
				Path:     *blobPrefix.Name,
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

// MakeDir creates a virtual directory by uploading an empty blob
// Azure Blob Storage uses virtual directories
func (d *AzureBlob) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) (model.Obj, error) {
	dirPath := path.Join(parentDir.GetPath(), dirName) + "/"
	blobClient := d.containerClient.NewBlockBlobClient(dirPath)
	_, err := blobClient.Upload(ctx, struct {
		*bytes.Reader
		io.Closer
	}{Reader: bytes.NewReader([]byte{}), Closer: io.NopCloser(nil)}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	return &model.Object{Path: dirPath, Name: dirName, IsFolder: true}, nil
}

// Move blob by copying and deleting the source
// https://learn.microsoft.com/rest/api/storageservices/copy-blob
func (d *AzureBlob) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	// Use Copy method to copy the object
	dstObj, err := d.Copy(ctx, srcObj, dstDir)
	if err != nil {
		return nil, err
	}

	// Delete the source object
	if err := d.Remove(ctx, srcObj); err != nil {
		return nil, err
	}

	return dstObj, nil
}

// Rename blob by moving it to a new name
func (d *AzureBlob) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	dstDir := &model.Object{Path: path.Dir(srcObj.GetPath())}
	return d.Move(ctx, srcObj, dstDir)
}

// Copy blob to destination directory
// https://learn.microsoft.com/rest/api/storageservices/copy-blob
func (d *AzureBlob) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	dstPath := path.Join(dstDir.GetPath(), srcObj.GetName())

	// Handle directory copying using flat listing
	if srcObj.IsDir() {
		srcPrefix := srcObj.GetPath()
		if !strings.HasSuffix(srcPrefix, "/") {
			srcPrefix += "/"
		}

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

			// Calculate destination path
			blobDstPath := path.Join(dstPath, relPath)

			// Copy the blob
			if err := d.copyFile(ctx, *blob.Name, blobDstPath); err != nil {
				return nil, fmt.Errorf("failed to copy %s: %w", *blob.Name, err)
			}
		}

		// Create directory marker at destination if needed
		if len(blobs) == 0 {
			blobClient := d.containerClient.NewBlockBlobClient(dstPath + "/")
			_, err = blobClient.Upload(ctx, struct {
				*bytes.Reader
				io.Closer
			}{Reader: bytes.NewReader([]byte{}), Closer: io.NopCloser(nil)}, nil)

			if err != nil {
				return nil, fmt.Errorf("failed to create directory marker: %w", err)
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
// https://learn.microsoft.com/rest/api/storageservices/delete-blob
func (d *AzureBlob) Remove(ctx context.Context, obj model.Obj) error {
	// Handle recursive directory deletion
	if obj.IsDir() {
		return d.deleteFolder(ctx, obj.GetPath())
	}

	// Delete single file
	return d.deleteFile(ctx, obj.GetPath(), false)
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

	// Upload with retry logic
	err := d.withRetry(func() error {
		_, err := blobClient.UploadStream(ctx, limitedStream, options)
		return err
	})
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

// progressTracker is used to track upload progress
type progressTracker struct {
	total          int64
	current        int64
	updateProgress driver.UpdateProgress
}

// Write implements io.Writer to track progress
func (pt *progressTracker) Write(p []byte) (n int, err error) {
	n = len(p)
	pt.current += int64(n)
	if pt.updateProgress != nil && pt.total > 0 {
		pt.updateProgress(float64(pt.current) * 100 / float64(pt.total))
	}
	return n, nil
}

// GetArchiveMeta returns archive file meta
func (d *AzureBlob) GetArchiveMeta(ctx context.Context, obj model.Obj, args model.ArchiveArgs) (model.ArchiveMeta, error) {
	return nil, errs.NotImplement
}

// ListArchive lists files in archive
func (d *AzureBlob) ListArchive(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) ([]model.Obj, error) {
	return nil, errs.NotImplement
}

// Extract extracts file from archive
func (d *AzureBlob) Extract(ctx context.Context, obj model.Obj, args model.ArchiveInnerArgs) (*model.Link, error) {
	return nil, errs.NotImplement
}

// ArchiveDecompress extracts archive to directory
func (d *AzureBlob) ArchiveDecompress(ctx context.Context, srcObj, dstDir model.Obj, args model.ArchiveDecompressArgs) ([]model.Obj, error) {
	return nil, errs.NotImplement
}

var _ driver.Driver = (*AzureBlob)(nil)
