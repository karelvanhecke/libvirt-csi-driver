package controller

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/digitalocean/go-libvirt"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
	"libvirt.org/go/libvirtxml"
)

func (cs *ControllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	c, err := cs.connectedClient()
	if err != nil {
		return nil, err
	}

	vol, err := c.StorageVolLookupByKey(req.GetVolumeId())
	if err != nil {
		if isVolNotFoundError(err) {
			return nil, grpcerr.NotFound(err)
		}
		return nil, grpcerr.Internal(err)
	}

	requestedBytes := req.CapacityRange.RequiredBytes
	_, volCapacity, _, err := c.StorageVolGetInfo(vol)
	if err != nil {
		return nil, grpcerr.Internal(err)
	}

	if c := int64(volCapacity); c > requestedBytes {
		return &csi.ControllerExpandVolumeResponse{CapacityBytes: c, NodeExpansionRequired: false}, nil
	}

	pool, err := c.StoragePoolLookupByVolume(vol)
	if err != nil {
		if isPoolNotFoundError(err) {
			return nil, grpcerr.InvalidArgument(err)
		}

		return nil, grpcerr.Internal(err)
	}
	_, _, _, poolCapacity, err := c.StoragePoolGetInfo(pool)
	if err != nil {
		return nil, grpcerr.Internal(err)
	}

	if requestedBytes > int64(poolCapacity) {
		return nil, grpcerr.InvalidArgument(ErrRequestExceedsPoolCapacity)
	}

	volXML, err := c.StorageVolGetXMLDesc(vol, 0)
	if err != nil {
		return nil, grpcerr.Internal(err)
	}

	volSpec := &libvirtxml.StorageVolume{}
	if err := volSpec.Unmarshal(volXML); err != nil {
		return nil, grpcerr.Internal(err)
	}

	dom, ok, err := cs.domainUsingVolume(volSpec)
	if err != nil {
		return nil, err
	}

	if !ok {
		if err := c.StorageVolResize(vol, uint64(requestedBytes), 0); err != nil {
			return nil, grpcerr.Internal(err)
		}
	} else {
		if err := c.DomainBlockResize(dom, volSpec.Target.Path, uint64(requestedBytes), libvirt.DomainBlockResizeBytes); err != nil {
			return nil, grpcerr.Internal(err)
		}
	}

	resp := &csi.ControllerExpandVolumeResponse{
		CapacityBytes: requestedBytes,
	}

	if cap := req.VolumeCapability; cap != nil {
		if err := cs.verifyVolumeCapability(cap); err != nil {
			return nil, grpcerr.InvalidArgument(err)
		}
		if _, ok := cap.AccessType.(*csi.VolumeCapability_Block); ok {
			resp.NodeExpansionRequired = false
		}
		resp.NodeExpansionRequired = true
	}

	return resp, nil
}

func (cs *ControllerServer) domainUsingVolume(vol *libvirtxml.StorageVolume) (dom libvirt.Domain, ok bool, err error) {
	c, err := cs.connectedClient()
	if err != nil {
		return libvirt.Domain{}, false, err
	}

	doms, _, err := c.ConnectListAllDomains(1, libvirt.ConnectListDomainsActive)
	if err != nil {
		return libvirt.Domain{}, false, grpcerr.Internal(err)
	}

	for _, dom := range doms {
		_, ok, _, err := cs.isAttachedToDomain(vol, dom)
		if err != nil {
			return libvirt.Domain{}, false, grpcerr.Internal(err)
		}

		if ok {
			return dom, true, nil
		}
	}

	return libvirt.Domain{}, false, nil
}
