package controller

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/digitalocean/go-libvirt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testPool        = "default-pool"
	testVol         = "default-vol"
	testVolCapacity = 1000000
)

func TestCreateVolume(t *testing.T) {
	cs := NewControllerServer(testClient(t), testPool)

	tests := []struct {
		Name             string
		VolName          string
		Capacity         int64
		ExpectedCapacity int64
	}{
		{
			Name:             "Existing",
			VolName:          testVol,
			ExpectedCapacity: testVolCapacity,
		},
		{
			Name:             "NewWithDefaultCapacity",
			VolName:          "new-vol-default-capacity",
			ExpectedCapacity: int64(cs.defaultCapacity),
		},
		{
			Name:             "NewWithRequestedCapacity",
			VolName:          "new-vol-requested-capacity",
			Capacity:         1000000000,
			ExpectedCapacity: 1000000000,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			req := &csi.CreateVolumeRequest{
				Name: test.VolName,
			}

			if c := test.Capacity; c != 0 {
				req.CapacityRange = &csi.CapacityRange{
					RequiredBytes: c,
				}
			}

			resp, err := cs.CreateVolume(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}

			v := resp.Volume
			if v == nil {
				t.Fatal("volume field is missing")
			}

			if v.VolumeId != filepath.Join("/", testPool, test.VolName) {
				t.Log("volume id does not match expected key")
				t.Fail()
			}

			if v.CapacityBytes != test.ExpectedCapacity {
				t.Log("capacity bytes does not match expected value")
				t.Fail()
			}
		})
	}
}

func TestUnsupportedAccessMode(t *testing.T) {
	cs := NewControllerServer(testClient(t), testPool)

	tests := []struct {
		Name    string
		VolName string
		Code    codes.Code
	}{
		{
			Name:    "NewVolume",
			VolName: t.Name(),
			Code:    codes.InvalidArgument,
		},
		{
			Name:    "ExistingVolume",
			VolName: testVol,
			Code:    codes.AlreadyExists,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			_, err := cs.CreateVolume(t.Context(), &csi.CreateVolumeRequest{
				Name: test.VolName,
				VolumeCapabilities: []*csi.VolumeCapability{{
					AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
					},
				}},
			})

			s, ok := status.FromError(err)
			if !ok {
				t.Fatalf("grpc error must be returned")
			}

			if s.Code() != test.Code {
				t.Log("incorrect error code")
				t.Fail()
			}
		})
	}
}

func TestDeleteVolume(t *testing.T) {
	cs := NewControllerServer(testClient(t), testPool)

	tests := []struct {
		Name  string
		VolId string
	}{
		{
			Name:  "ExistingVolume",
			VolId: filepath.Join("/", testPool, testVol),
		},
		{
			Name:  "NonExistingVolume",
			VolId: t.Name(),
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			_, err := cs.DeleteVolume(t.Context(), &csi.DeleteVolumeRequest{
				VolumeId: test.VolId,
			})

			if err != nil {
				t.Fail()
			}
		})
	}
}

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

func testClient(t *testing.T) *libvirt.Libvirt {
	wd, err := os.Getwd()
	if err != nil {
		setupFail(t, err)
	}

	uri, err := url.Parse(fmt.Sprintf("test://%s/%s", wd, "testdata/driver.xml"))
	if err != nil {
		setupFail(t, err)
	}

	l, err := libvirt.ConnectToURI(uri)
	if err != nil {
		setupFail(t, err)
	}
	return l
}

func setupFail(t *testing.T, err error) {
	log.Fatalf("Failed to setup test %s: %s", t.Name(), err)
}
