package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/digitalocean/go-libvirt"
	"github.com/google/uuid"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"libvirt.org/go/libvirtxml"
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
		if err := cs.verifyVolumeCapability(cap); err != nil {
			return nil, grpcerr.AlreadyExists(err)
		}

		if cap.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY || req.Readonly {
			if disk.ReadOnly == nil {
				return nil, grpcerr.AlreadyExists(ErrVolumeMustBeReadOnly)
			}
		}

		return &csi.ControllerPublishVolumeResponse{PublishContext: map[string]string{"wwn": disk.WWN}}, nil
	}

	if err := cs.verifyVolumeCapability(cap); err != nil {
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

func (cs *ControllerServer) attachToDomain(vol *libvirtxml.StorageVolume, dom libvirt.Domain, wwn, dev string) error {
	disk, err := domainDisk(vol, wwn, dev)
	if err != nil {
		return err
	}

	c, err := cs.connectedClient()
	if err != nil {
		return err
	}

	if err := c.DomainAttachDevice(dom, disk); err == nil {
		return nil
	}

	if !isResourceBusyError(err) {
		return grpcerr.Internal(err)
	}

	dom, ok, err := cs.domainUsingVolume(vol)
	if err != nil {
		return err
	}

	if !ok {
		return grpcerr.Internal(err)
	}

	id, err := uuid.ParseBytes(dom.UUID[:])
	if err != nil {
		return grpcerr.Internal(err)
	}

	return status.Error(codes.FailedPrecondition, id.String())
}

func (cs *ControllerServer) volumeSpecByID(id string) (*libvirtxml.StorageVolume, error) {
	c, err := cs.connectedClient()
	if err != nil {
		return nil, err
	}

	vol, err := c.StorageVolLookupByKey(id)
	if err != nil {
		if isVolNotFoundError(err) {
			return nil, grpcerr.NotFound(err)
		}
		return nil, grpcerr.Internal(err)
	}

	volXML, err := c.StorageVolGetXMLDesc(vol, 0)
	if err != nil {
		return nil, grpcerr.Internal(err)
	}
	volSpec := &libvirtxml.StorageVolume{}
	if err := volSpec.Unmarshal(volXML); err != nil {
		return nil, grpcerr.Internal(err)
	}

	return volSpec, nil
}

func (cs *ControllerServer) domainByID(id string) (libvirt.Domain, error) {
	c, err := cs.connectedClient()
	if err != nil {
		return libvirt.Domain{}, err
	}

	uuid, err := uuid.Parse(id)
	if err != nil {
		return libvirt.Domain{}, grpcerr.InvalidArgument(err)
	}

	dom, err := c.DomainLookupByUUID(libvirt.UUID(uuid))
	if err != nil {
		e, ok := err.(libvirt.Error)
		if !ok || e.Code != uint32(libvirt.ErrNoDomain) {
			return libvirt.Domain{}, grpcerr.Internal(err)
		}

		return libvirt.Domain{}, grpcerr.NotFound(err)
	}

	return dom, nil
}

func domainDisk(vol *libvirtxml.StorageVolume, wwn string, dev string) (string, error) {
	diskSpec := &libvirtxml.DomainDisk{
		Device: "disk",
		Driver: &libvirtxml.DomainDiskDriver{
			Name:        "qemu",
			Type:        vol.Target.Format.Type,
			Discard:     "unmap",
			DetectZeros: "unmap",
		},
		WWN: wwn,
		Target: &libvirtxml.DomainDiskTarget{
			Dev: dev,
			Bus: "scsi",
		},
	}

	switch vol.Type {
	case "file":
		diskSpec.Source.File = &libvirtxml.DomainDiskSourceFile{
			File: vol.Target.Path,
		}
	case "block":
		diskSpec.Source.Block = &libvirtxml.DomainDiskSourceBlock{
			Dev: vol.Target.Path,
		}
	default:
		return "", grpcerr.InvalidArgument(fmt.Errorf("%s volume type is not supported", vol.Type))
	}

	disk, err := diskSpec.Marshal()
	if err != nil {
		return "", grpcerr.Internal(err)
	}

	return disk, nil
}

func nextDev(used []string) (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyz"

	for i, j := 0, -1; i < 26 || j < 26; i++ {
		if i == 26 {
			i = 0
			j++
		}

		f := ""
		if j > -1 {
			f = string(letters[j])
		}

		if id := f + string(letters[i]); !slices.Contains(used, id) {
			return "sd" + id, nil
		}
	}

	return "", grpcerr.Internal(ErrGenerateDeviceID)
}

func generateWWN() (string, error) {
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return "", grpcerr.Internal(err)
	}
	return hex.EncodeToString(randBytes), nil
}
