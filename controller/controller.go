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

	defaultCapacity uint64

	fsTypes     []string
	accessModes []csi.VolumeCapability_AccessMode_Mode
}

func NewControllerServer(client *libvirt.Libvirt, defaultPool string) *ControllerServer {
	return &ControllerServer{
		client:          client,
		defaultPool:     defaultPool,
		defaultCapacity: 1073741824,
		fsTypes:         []string{"ext4", "xfs"},
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
	name := req.GetName()
	params := req.GetParameters()

	pool, err := cs.lookupStoragePool(params)
	if err != nil {
		return nil, err
	}

	c, err := cs.connectedClient()
	if err != nil {
		return nil, err
	}

	vol, err := c.StorageVolLookupByName(pool, name)
	if err == nil {
		return cs.createVolumeExists(vol, req)
	}
	e, ok := err.(libvirt.Error)
	if !ok || e.Code != uint32(libvirt.ErrNoStorageVol) {
		return nil, grpcerr.Internal(err)
	}

	if err := cs.verifyVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, grpcerr.InvalidArgument(err)
	}

	spec := &libvirtxml.StorageVolume{
		Name: name,
		Capacity: &libvirtxml.StorageVolumeSize{
			Value: cs.determineCapacity(req.CapacityRange),
		},
	}

	xml, err := spec.Marshal()
	if err != nil {
		return nil, grpcerr.Internal(err)
	}

	vol, err = c.StorageVolCreateXML(pool, xml, 0)
	if err != nil {
		return nil, grpcerr.Internal(err)
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      vol.Key,
			CapacityBytes: int64(spec.Capacity.Value),
		},
	}, nil
}

func (cs *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	c, err := cs.connectedClient()
	if err != nil {
		return nil, err
	}

	vol, err := c.StorageVolLookupByKey(req.VolumeId)
	if err == nil {
		if err := c.StorageVolDelete(vol, 0); err != nil {
			return nil, grpcerr.Internal(err)
		}
		return &csi.DeleteVolumeResponse{}, nil
	}

	e, ok := err.(libvirt.Error)
	if !ok || e.Code != uint32(libvirt.ErrNoStorageVol) {
		return nil, grpcerr.Internal(err)
	}
	return &csi.DeleteVolumeResponse{}, nil
}

// Ensure the Libvirt client is connected.
// Returns grpc error code unavailable on connection issues.
func (cs *ControllerServer) connectedClient() (*libvirt.Libvirt, error) {
	if cs.client.IsConnected() {
		return cs.client, nil
	}
	err := cs.client.Connect()
	if err != nil {
		return nil, grpcerr.Unavailable(err)
	}
	return cs.client, nil
}

// Handle existing volume.
func (cs *ControllerServer) createVolumeExists(vol libvirt.StorageVol, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if err := cs.verifyVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, grpcerr.AlreadyExists(err)
	}

	c, err := cs.connectedClient()
	if err != nil {
		return nil, err
	}

	_, capacity, _, err := c.StorageVolGetInfo(vol)
	if err != nil {
		return nil, grpcerr.Internal(err)
	}

	if err := verifyCapacity(int64(capacity), req.CapacityRange); err != nil {
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

func (cs *ControllerServer) determineCapacity(capRange *csi.CapacityRange) uint64 {
	if capRange == nil {
		return cs.defaultCapacity
	}

	if rb := capRange.RequiredBytes; rb != 0 {
		return uint64(rb)
	}

	if lb := capRange.LimitBytes; lb != 0 && lb < int64(cs.defaultCapacity) {
		return uint64(lb)
	}

	return cs.defaultCapacity
}

func (cs *ControllerServer) lookupStoragePool(params map[string]string) (libvirt.StoragePool, error) {
	name := params["pool"]
	if name == "" {
		name = cs.defaultPool
	}

	c, err := cs.connectedClient()
	if err != nil {
		return libvirt.StoragePool{}, err
	}

	pool, err := c.StoragePoolLookupByName(name)
	if err == nil {
		return pool, nil
	}
	e, ok := err.(libvirt.Error)
	if ok && e.Code == uint32(libvirt.ErrNoStoragePool) {
		return libvirt.StoragePool{}, grpcerr.InvalidArgument(err)
	}
	return libvirt.StoragePool{}, grpcerr.Internal(err)
}

func volumeId(vol libvirt.StorageVol) string {
	return vol.Key
}

func verifyCapacity(cap int64, capRange *csi.CapacityRange) error {
	if capRange == nil {
		return nil
	}
	if rb := capRange.RequiredBytes; rb != 0 && cap < rb {
		return errors.New("existing volume does not meet required capacity")
	}

	if lb := capRange.LimitBytes; lb != 0 && cap > lb {
		return errors.New("existing volume exceeds capacity limit")
	}

	return nil
}
