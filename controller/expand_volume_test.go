// Copyright 2026 Karel Van Hecke
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"libvirt.org/go/libvirtxml"
)

func TestControllerExpandVolume(t *testing.T) {
	volume := &libvirtxml.StorageVolume{
		Name:     "expand-test",
		Capacity: &libvirtxml.StorageVolumeSize{Value: 1},
	}

	te := newTestEnv(t, withVolumes(volume))

	volumeID := te.volumeID(volume.Name)

	cs := NewControllerServer(te.client, te.poolName())

	req := &csi.ControllerExpandVolumeRequest{
		VolumeId: volumeID,
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 8192,
			LimitBytes:    8192,
		},
	}

	t.Run("Expanded", func(t *testing.T) {
		resp, err := cs.ControllerExpandVolume(t.Context(), req)

		if err != nil {
			t.Fatal(err)
		}

		capacity := te.volumeCapacity(volumeID)
		if capacity < req.CapacityRange.RequiredBytes {
			t.Log("capacity is smaller than required capacity")
			t.Fail()
		}

		if capacity > req.CapacityRange.LimitBytes {
			t.Log("capacity exceeds requested capacity limit")
			t.Fail()
		}

		if cb := resp.CapacityBytes; cb != capacity {
			t.Logf("expected capacity %d, got %d", capacity, cb)
			t.Fail()
		}
	})

	errTests := []struct {
		Name         string
		Req          *csi.ControllerExpandVolumeRequest
		ExpectedCode codes.Code
	}{
		{
			Name: "NotFound",
			Req: &csi.ControllerExpandVolumeRequest{
				VolumeId:      "fake",
				CapacityRange: &csi.CapacityRange{RequiredBytes: 8192, LimitBytes: 8192},
			},
			ExpectedCode: codes.NotFound,
		},
		{
			Name: "ExceedsCapabilities",
			Req: &csi.ControllerExpandVolumeRequest{
				VolumeId: volumeID,
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 8192,
					LimitBytes:    8192,
				},
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
					},
				},
			},
			ExpectedCode: codes.InvalidArgument,
		},
	}

	for _, errTest := range errTests {
		t.Run(errTest.Name, func(t *testing.T) {
			_, err := cs.ControllerExpandVolume(t.Context(), errTest.Req)
			if err == nil {
				t.Fatal("expected error")
			}
			st, ok := status.FromError(err)
			if !ok {
				t.Fatal("expected GRPC error")
			}
			if got, expected := st.Code(), errTest.ExpectedCode; got != expected {
				t.Fatalf("expected code %d, got %d", expected, got)
			}
		})
	}
}
