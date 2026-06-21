package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/digitalocean/go-libvirt"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
	"libvirt.org/go/libvirtxml"
)

type ControllerServer struct {
	*csi.UnimplementedControllerServer

	client      *libvirt.Libvirt
	defaultPool string

	defaultCapacity    libvirtxml.StorageVolumeSize
	defaultThinVolumes bool

	fsTypes     []string
	accessModes []csi.VolumeCapability_AccessMode_Mode
}

func NewControllerServer(client *libvirt.Libvirt, defaultPool string) *ControllerServer {
	return &ControllerServer{
		client:      client,
		defaultPool: defaultPool,
		defaultCapacity: libvirtxml.StorageVolumeSize{
			Unit:  "GiB",
			Value: 1,
		},
		defaultThinVolumes: true,
		fsTypes:            []string{"ext4", "xfs"},
		accessModes: []csi.VolumeCapability_AccessMode_Mode{
			csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
			csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
		},
	}
}

func (cs *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	err := cs.connectedClient()
	if err != nil {
		return nil, err
	}

	name := req.GetName()
	params := req.GetParameters()

	pool, err := cs.lookupStoragePool(params)
	if err != nil {
		return nil, err
	}

	vol, err := cs.client.StorageVolLookupByName(pool, name)
	if err == nil {
		return cs.createVolumeExists(vol, req)
	}
	e, ok := err.(*libvirt.Error)
	if !ok || e.Code != uint32(libvirt.ErrNoStorageVol) {
		return nil, grpcerr.Internal(err)
	}

	//TODO: volume creation

	return &csi.CreateVolumeResponse{}, nil
}

// Ensure the Libvirt client is connected.
// Returns grpc error code unavailable on connection issues.
func (cs *ControllerServer) connectedClient() error {
	if cs.client.IsConnected() {
		return nil
	}
	err := cs.client.Connect()
	if err != nil {
		return grpcerr.Unavailable(err)
	}
	return nil
}

// Handle existing volume.
func (cs *ControllerServer) createVolumeExists(vol libvirt.StorageVol, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if err := cs.verifyVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, grpcerr.AlreadyExists(err)
	}
	_, capacity, _, err := cs.client.StorageVolGetInfo(vol)
	if err != nil {
		return nil, grpcerr.Internal(err)
	}

	capacityRange := req.GetCapacityRange()
	if c := int64(capacity); c < capacityRange.GetRequiredBytes() || c > capacityRange.GetLimitBytes() {
		return nil, grpcerr.AlreadyExists(err)
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			CapacityBytes: int64(capacity),
			VolumeId:      volumeId(vol),
		},
	}, nil
}

// Make sure all requested volume capabilities are supported.
func (cs *ControllerServer) verifyVolumeCapabilities(caps []*csi.VolumeCapability) error {
	for _, cap := range caps {
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

func (cs *ControllerServer) lookupStoragePool(params map[string]string) (libvirt.StoragePool, error) {
	name := params["pool"]
	if name == "" {
		name = cs.defaultPool
	}

	pool, err := cs.client.StoragePoolLookupByName(name)
	if err == nil {
		return pool, nil
	}
	e, ok := err.(*libvirt.Error)
	if ok && e.Code == uint32(libvirt.ErrNoStoragePool) {
		return libvirt.StoragePool{}, grpcerr.InvalidArgument(err)
	}
	return libvirt.StoragePool{}, grpcerr.Internal(err)
}

func volumeId(vol libvirt.StorageVol) string {
	return vol.Key
}
