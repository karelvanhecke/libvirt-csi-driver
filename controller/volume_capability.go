package controller

import (
	"errors"
	"fmt"
	"slices"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

// Verify volume capability is supported.
func (cs *ControllerServer) verifyVolumeCapability(cap *csi.VolumeCapability) error {
	switch cap.AccessType.(type) {
	case *csi.VolumeCapability_Block: // Does not contain anything
	case *csi.VolumeCapability_Mount:
		// Type is already enforced by type switch
		//nolint:errcheck
		cs.verifyVolumeCapabilityMount(cap.AccessType.(*csi.VolumeCapability_Mount).Mount)
	default:
		return errors.New("unsupported access type")
	}

	mode := cap.AccessMode.Mode
	if !slices.Contains(cs.accessModes, mode) {
		return fmt.Errorf("access mode %s is not supported", mode)
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
			return errors.New("unsupported filesystem")
		}
	}

	if mount.MountFlags != nil {
		return errors.New("mount flags are not supported")
	}

	return nil
}
