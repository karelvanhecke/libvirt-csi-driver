package controller

import (
	"context"
	"errors"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/digitalocean/go-libvirt"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
	"libvirt.org/go/libvirtxml"
)

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
	if !isVolNotFoundError(err) {
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
	if !isVolNotFoundError(err) {
		return nil, grpcerr.Internal(err)
	}

	return &csi.DeleteVolumeResponse{}, nil
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
