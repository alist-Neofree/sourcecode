package azure_blob

import (
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/op"
)

type Addition struct {
	driver.RootPath
	Name          string `json:"name" required:"true"`
	Key           string `json:"key" required:"true"`
	Endpoint      string `json:"endpoint" required:"true"`
	Container     string `json:"container" required:"true"`
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
