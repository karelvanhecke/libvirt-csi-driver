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

func TestCreateVolume(t *testing.T) {
	existingVolume := &libvirtxml.StorageVolume{
		Name:     "existing",
		Capacity: &libvirtxml.StorageVolumeSize{Value: 1},
	}

	te := newTestEnv(t, withVolumes(existingVolume))

	cs := NewControllerServer(te.client, te.poolName())

	tests := []struct {
		Name    string
		Request *csi.CreateVolumeRequest
	}{
		{
			Name: "NewWithDefaults",
			Request: &csi.CreateVolumeRequest{
				Name: "default",
			},
		},
		{
			Name: "NewWithExactCapacityRange",
			Request: &csi.CreateVolumeRequest{
				Name: "capacity-range-exact",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 1,
					LimitBytes:    1,
				},
			},
		},
		{
			Name: "NewWithCapacityRangeLimitOnly",
			Request: &csi.CreateVolumeRequest{
				Name: "capacity-range-limit",
				CapacityRange: &csi.CapacityRange{
					LimitBytes: 1,
				},
			},
		},
		{
			Name: "AlreadyExists",
			Request: &csi.CreateVolumeRequest{
				Name: existingVolume.Name,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			resp, err := cs.CreateVolume(t.Context(), test.Request)
			if err != nil {
				t.Fatal(err)
			}

			if resp.Volume == nil {
				t.Fatalf("Volume should not be nil in reponse")
			}

			id := resp.Volume.VolumeId
			if !te.volumeExists(id) {
				t.Fatalf("Expected volume with ID %s does not exist", id)
			}

			capacity := te.volumeCapacity(id)
			if cb := resp.Volume.CapacityBytes; cb != capacity {
				t.Fatalf("Volume capacity in response was %d, while the volume is %d", cb, capacity)
			}

			if cr := test.Request.CapacityRange; cr != nil {
				if rb := cr.RequiredBytes; rb != 0 && capacity < rb {
					t.Logf("required capacity is %d, got %d", rb, capacity)
					t.Fail()
				}

				if lb := cr.LimitBytes; lb != 0 && capacity > lb {
					t.Logf("capacity limit is %d, got %d", lb, capacity)
					t.Fail()
				}
			}
		})
	}
}

func TestCreateVolumeErrors(t *testing.T) {
	existingVolume := &libvirtxml.StorageVolume{
		Name:     "existing",
		Capacity: &libvirtxml.StorageVolumeSize{Value: 2},
	}

	te := newTestEnv(t, withVolumes(existingVolume))

	cs := NewControllerServer(te.client, te.poolName())

	tests := []struct {
		Name          string
		Req           *csi.CreateVolumeRequest
		ExpectedCode  codes.Code
		ExpectedError error
	}{
		{
			Name: "ExistingVolumeRequiredCapacity",
			Req: &csi.CreateVolumeRequest{
				Name: "existing",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 3,
					LimitBytes:    3,
				},
			},
			ExpectedCode:  codes.AlreadyExists,
			ExpectedError: ErrExistingVolumeRequiredCapacity,
		},
		{
			Name: "ExistingVolumeCapacityLimit",
			Req: &csi.CreateVolumeRequest{
				Name: "existing",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 1,
					LimitBytes:    1,
				},
			},
			ExpectedCode:  codes.AlreadyExists,
			ExpectedError: ErrExistingVolumeCapacityLimit,
		},
		{
			Name: "ExistingVolumeAccessMode",
			Req: &csi.CreateVolumeRequest{
				Name: "existing",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 2,
					LimitBytes:    2,
				},
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessType: &csi.VolumeCapability_Block{},
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
						},
					},
				},
			},
			ExpectedCode:  codes.AlreadyExists,
			ExpectedError: ErrUnsupportedAccessMode,
		},
		{
			Name: "ExistingVolumeFilesystem",
			Req: &csi.CreateVolumeRequest{
				Name: "existing",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 2,
					LimitBytes:    2,
				},
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessType: &csi.VolumeCapability_Mount{
							Mount: &csi.VolumeCapability_MountVolume{
								FsType: "fakefs",
							},
						},
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
						},
					},
				},
			},
			ExpectedCode:  codes.AlreadyExists,
			ExpectedError: ErrUnsupportedFilesystem,
		},
		{
			Name: "NewVolumeAccessMode",
			Req: &csi.CreateVolumeRequest{
				Name: "new",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessType: &csi.VolumeCapability_Block{},
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
						},
					},
				},
			},
			ExpectedCode:  codes.InvalidArgument,
			ExpectedError: ErrUnsupportedAccessMode,
		},
		{
			Name: "NewVolumeFilesystem",
			Req: &csi.CreateVolumeRequest{
				Name: "new",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessType: &csi.VolumeCapability_Mount{
							Mount: &csi.VolumeCapability_MountVolume{
								FsType: "fakefs",
							},
						},
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
						},
					},
				},
			},
			ExpectedCode:  codes.InvalidArgument,
			ExpectedError: ErrUnsupportedFilesystem,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			_, err := cs.CreateVolume(t.Context(), test.Req)
			if err == nil {
				t.Log("expected error")
				t.Fail()
			}
			st, ok := status.FromError(err)
			if !ok {
				t.Fatal("expected grpc error")
			}
			if got, expected := st.Code(), test.ExpectedCode; got != expected {
				t.Logf("expected code %d, got %s", expected, got)
				t.Fail()
			}
			if got, expected := st.Message(), test.ExpectedError.Error(); got != expected {
				t.Logf("expected error message '%s', got `%s`", expected, got)
			}
		})
	}
}

func TestDeleteVolume(t *testing.T) {
	volume := &libvirtxml.StorageVolume{
		Name:     "to-be-deleted",
		Capacity: &libvirtxml.StorageVolumeSize{Value: 1},
	}

	te := newTestEnv(t, withVolumes(volume))
	cs := NewControllerServer(te.client, te.poolName())

	volumeID := te.volumeID(volume.Name)
	req := &csi.DeleteVolumeRequest{
		VolumeId: volumeID,
	}

	resp, err := cs.DeleteVolume(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}

	if resp == nil {
		t.Fail()
	}

	if te.volumeExists(volumeID) {
		t.Fail()
	}

	t.Run("VolumeAlreadyDeleted", func(t *testing.T) {
		resp, err = cs.DeleteVolume(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}

		if resp == nil {
			t.Fail()
		}
	})
}
