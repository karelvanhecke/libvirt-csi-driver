package controller

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
)

func (cs *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	vol, err := cs.volumeSpecByID(req.GetVolumeId())
	if err != nil {
		return nil, err
	}

	dom, err := cs.domainByID(req.GetNodeId())
	if err != nil {
		return nil, err
	}

	cap := req.GetVolumeCapability()

	disk, ok, usedDevs, err := cs.isAttachedToDomain(vol, dom)
	if err != nil {
		return nil, err
	}
	if ok {
		if err := cs.verifyVolumeCapabilities([]*csi.VolumeCapability{cap}); err != nil {
			return nil, grpcerr.AlreadyExists(err)
		}

		if cap.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY || req.Readonly {
			if disk.ReadOnly == nil {
				return nil, grpcerr.AlreadyExists(ErrVolumeMustBeReadOnly)
			}
		}

		return &csi.ControllerPublishVolumeResponse{PublishContext: map[string]string{"wwn": disk.WWN}}, nil
	}

	if err := cs.verifyVolumeCapabilities([]*csi.VolumeCapability{cap}); err != nil {
		return nil, grpcerr.InvalidArgument(err)
	}

	if cap.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY && !req.Readonly {
		return nil, grpcerr.InvalidArgument(ErrVolumeMustBeReadOnly)
	}

	dev, err := nextDev(usedDevs)
	if err != nil {
		return nil, err
	}

	wwn, err := generateWWN()
	if err != nil {
		return nil, err
	}

	if err := cs.attachToDomain(vol, dom, wwn, dev); err != nil {
		return nil, err
	}

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			"wwn": wwn,
		},
	}, nil
}

func (cs *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	vol, err := cs.volumeSpecByID(req.GetVolumeId())
	if err != nil {
		return nil, err
	}

	dom, err := cs.domainByID(req.GetNodeId())
	if err != nil {
		return nil, err
	}

	disk, ok, _, err := cs.isAttachedToDomain(vol, dom)
	if err != nil {
		return nil, err
	}

	diskXML, err := disk.Marshal()
	if err != nil {
		return nil, grpcerr.Internal(err)
	}

	if ok {
		c, err := cs.connectedClient()
		if err != nil {
			return nil, err
		}

		if err := c.DomainDetachDevice(dom, diskXML); err != nil {
			return nil, grpcerr.Internal(err)
		}
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}
