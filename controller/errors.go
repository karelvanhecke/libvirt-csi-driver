// Copyright 2026 Karel Van Hecke
// SPDX-License-Identifier: Apache-2.0

package controller

import "errors"

// Controller server errors
var (
	// Capacity errors
	ErrExistingVolumeRequiredCapacity = errors.New("existing volume does not meet required capacity")
	ErrExistingVolumeCapacityLimit    = errors.New("existing volume exceeds capacity limit")
	ErrRequestExceedsPoolCapacity     = errors.New("request exceeds available storage pool capacity")

	// Volume capability errors
	ErrUnsupportedAccessType  = errors.New("unsupported access type")
	ErrUnsupportedAccessMode  = errors.New("unsupported access mode")
	ErrUnsupportedFilesystem  = errors.New("unsupported filesystem")
	ErrMountFlagsNotSupported = errors.New("mount flags are not supported")
	ErrVolumeMustBeReadOnly   = errors.New("volume must be read only")

	// Device ID generation
	ErrGenerateDeviceID = errors.New("could not generate device ID")
)
