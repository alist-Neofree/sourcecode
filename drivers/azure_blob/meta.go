package azure_blob

import (
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/op"
)

type Addition struct {
	Endpoint      string `json:"endpoint" required:"true" default:"https://<accountname>.blob.core.windows.net/" help:"The full endpoint URL for Azure Storage, including the account name."`
	AccessKey     string `json:"access_key" required:"true" help:"The access key for Azure Storage, used for authentication."`
	ContainerName string `json:"container_name" required:"true" help:"The name of the container in Azure Storage (created in the Azure portal)."`
	SignURLExpire int    `json:"sign_url_expire" type:"number" default:"4" help:"The expiration time for SAS URLs, in hours."`
}

var config = driver.Config{
	Name:        "AzureBlob",
	LocalSort:   true,
	CheckStatus: true,
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &AzureBlob{
			config: config,
		}
	})
}
