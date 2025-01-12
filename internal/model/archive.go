package model

type ObjTree interface {
	Obj
	GetChildren() []ObjTree
}

type ArchiveMeta interface {
	GetComment() string
	// IsEncrypted means if the content of the archive requires a password to access
	// GetArchiveMeta should return errs.WrongArchivePassword if the meta-info is also encrypted,
	// and the provided password is empty.
	IsEncrypted() bool
	// GetTree directly returns the full folder structure
	// returns nil if the folder structure should be acquired by calling driver.ArchiveReader.ListArchive
	GetTree() []ObjTree
}
