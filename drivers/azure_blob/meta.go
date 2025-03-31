package azure_blob

import (
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/op"
)

type Addition struct {
	driver.RootID `required:"false" help:"the container name"`
	AccessKey     string `json:"access_key" required:"true" help:"Azure Storage account key"`
	Endpoint      string `json:"endpoint" required:"true" default:"https://<account_name>.blob.core.windows.net/"`
	SignURLExpire int    `json:"sign_url_expire" type:"number" default:"4" help:"SAS URL expiration time in hours"`
}

var config = driver.Config{
	Name:        "AzureBlob",
	DefaultRoot: "/",
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
