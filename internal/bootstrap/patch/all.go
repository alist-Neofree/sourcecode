package patch

import "github.com/alist-org/alist/v3/internal/bootstrap/patch/v3_41_0"

type VersionPatches struct {
	Version string
	Patches []func()
}

var UpgradePatches = []VersionPatches{
	{
		Version: "v3.41.0",
		Patches: []func(){
			v3_41_0.GrantAdminPermissions,
		},
	},
}
