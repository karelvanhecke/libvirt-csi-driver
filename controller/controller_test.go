package controller

import (
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"libvirt.org/go/libvirtxml"
)

func TestValidateVolumeCapabilities(t *testing.T) {
	volume := &libvirtxml.StorageVolume{
		Name:     "test-volume",
		Capacity: &libvirtxml.StorageVolumeSize{Value: 1},
	}
	te := newTestEnv(t, withVolumes(volume))

	volumeID := te.volumeID(volume.Name)

	cs := NewControllerServer(te.client, te.poolName())

	capabilities := []*csi.VolumeCapability{
		{
			AccessType: &csi.VolumeCapability_Block{},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER},
		},
		{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER},
		},
	}

	t.Run("Confirmed", func(t *testing.T) {
		resp, err := cs.ValidateVolumeCapabilities(t.Context(), &csi.ValidateVolumeCapabilitiesRequest{
			VolumeId:           volumeID,
			VolumeCapabilities: capabilities,
		})
		if err != nil {
			t.Fatal(err)
		}

		if resp.Confirmed == nil {
			t.Fatal("confirmed should be set")
		}

		if len(resp.Confirmed.VolumeCapabilities) != len(capabilities) {
			t.Fatal("all supported capabilities should be returned")
		}
	})

	t.Run("VolumeNotFound", func(t *testing.T) {
		_, err := cs.ValidateVolumeCapabilities(t.Context(), &csi.ValidateVolumeCapabilitiesRequest{
			VolumeId:           "non-existing",
			VolumeCapabilities: capabilities,
		})
		if err == nil {
			t.Fatal("expected error")
		}
		st, ok := status.FromError(err)
		if !ok {
			t.Fatal("expected GRPC error")
		}
		if got, expected := st.Code(), codes.NotFound; got != expected {
			t.Fatalf("expected code %d, got %d", expected, got)
		}
	})

	t.Run("UnsupportedCapability", func(t *testing.T) {
		_, err := cs.ValidateVolumeCapabilities(t.Context(), &csi.ValidateVolumeCapabilitiesRequest{
			VolumeId: volumeID,
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
					},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error")
		}
		st, ok := status.FromError(err)
		if !ok {
			t.Fatal("expected GRPC error")
		}
		if got, expected := st.Code(), codes.InvalidArgument; got != expected {
			t.Fatalf("expected code %d, got %d", expected, got)
		}
	})
}
