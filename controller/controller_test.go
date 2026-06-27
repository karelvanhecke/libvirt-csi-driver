package controller

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestValidateVolumeCapabilitiesVolNotFound(t *testing.T) {
	cs := NewControllerServer(testClient(t), testPool)

	_, err := cs.ValidateVolumeCapabilities(t.Context(), &csi.ValidateVolumeCapabilitiesRequest{VolumeId: t.Name()})
	if err == nil {
		t.Log("error should not be nil")
		t.FailNow()
	}

	s, ok := status.FromError(err)
	if !ok {
		t.Log("error should contain a grpc status")
		t.FailNow()
	}

	if s.Code() != codes.NotFound {
		t.Log("status code should be NotFound")
		t.Fail()
	}
}

func TestValidateVolumeCapabilitiesConfirmed(t *testing.T) {
	cs := NewControllerServer(testClient(t), testPool)

	expectedCapability := &csi.VolumeCapability{
		AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
		AccessMode: &csi.VolumeCapability_AccessMode{
			Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		},
	}

	resp, err := cs.ValidateVolumeCapabilities(t.Context(), &csi.ValidateVolumeCapabilitiesRequest{
		VolumeId: filepath.Join("/", testPool, testVol),
		VolumeCapabilities: []*csi.VolumeCapability{
			expectedCapability,
		}})
	if err != nil {
		t.Log("error should be nil")
		t.FailNow()
	}

	if resp.Confirmed == nil {
		t.Log("call should return confirmed")
		t.FailNow()
	}

	if resp.Confirmed.VolumeCapabilities == nil {
		t.Log("supported volume capabilities should be returned")
		t.FailNow()
	}

	if !slices.ContainsFunc(resp.Confirmed.VolumeCapabilities, func(cap *csi.VolumeCapability) bool {
		_, ok := cap.AccessType.(*csi.VolumeCapability_Mount)
		if !ok {
			return false
		}
		if cap.AccessMode.Mode != expectedCapability.GetAccessMode().GetMode() {
			return false
		}
		return true
	}) {
		t.Log("supported capability not found in confirmation")
		t.Fail()
	}
}

func TestValidateVolumeCapabilitiesUnsupported(t *testing.T) {
	cs := NewControllerServer(testClient(t), testPool)

	_, err := cs.ValidateVolumeCapabilities(t.Context(), &csi.ValidateVolumeCapabilitiesRequest{
		VolumeId: filepath.Join("/", testPool, testVol),
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
				},
			},
		}})
	if err == nil {
		t.Log("error should not be nil")
		t.FailNow()
	}

	s, ok := status.FromError(err)
	if !ok {
		t.Log("error should contain a grpc status")
		t.FailNow()
	}

	if s.Code() != codes.InvalidArgument {
		t.Log("status code should be InvalidArgument")
		t.Fail()
	}
}
