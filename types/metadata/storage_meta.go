// Code generated by go generate internal/cmd/metadata; DO NOT EDIT.
package metadata

import (
	"github.com/Xuanwo/storage/pkg/storageclass"
)

var _ storageclass.Type

// All available metadata.
const (
	StorageMetaLocation = "location"
)

// GetLocation will get location value from metadata.
func (m StorageMeta) GetLocation() (string, bool) {
	v, ok := m.m[StorageMetaLocation]
	if !ok {
		return "", false
	}
	return v.(string), true
}

// MustGetLocation will get location value from metadata.
func (m StorageMeta) MustGetLocation() string {
	return m.m[StorageMetaLocation].(string)
}

// SetLocation will set location value into metadata.
func (m StorageMeta) SetLocation(v string) StorageMeta {
	m.m[StorageMetaLocation] = v
	return m
}
