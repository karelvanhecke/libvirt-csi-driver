// Copyright 2026 Karel Van Hecke
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"slices"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

// Verify volume capability is supported.
func (cs *ControllerServer) verifyVolumeCapability(cap *csi.VolumeCapability) error {
	switch cap.AccessType.(type) {
	case *csi.VolumeCapability_Block: // Does not contain anything
	case *csi.VolumeCapability_Mount:
		if err := cs.verifyVolumeCapabilityMount(cap.AccessType.(*csi.VolumeCapability_Mount).Mount); err != nil {
			return err
		}
	default:
		return ErrUnsupportedAccessType
	}

	if !slices.Contains(cs.accessModes, cap.AccessMode.Mode) {
		return ErrUnsupportedAccessMode
	}
	return nil
}

// Make sure all requested volume capabilities are supported.
func (cs *ControllerServer) verifyVolumeCapabilities(caps []*csi.VolumeCapability) error {
	for _, cap := range caps {
		if err := cs.verifyVolumeCapability(cap); err != nil {
			return err
		}
	}

	return nil
}

// Verify requested mount.
func (cs *ControllerServer) verifyVolumeCapabilityMount(mount *csi.VolumeCapability_MountVolume) error {
	if fs := mount.FsType; fs != "" {
		if !slices.Contains(cs.fsTypes, fs) {
			return ErrUnsupportedFilesystem
		}
	}

	if mount.MountFlags != nil {
		return ErrMountFlagsNotSupported
	}

	return nil
}
