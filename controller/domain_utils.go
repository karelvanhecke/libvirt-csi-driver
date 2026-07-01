package controller

import (
	"fmt"

	"github.com/digitalocean/go-libvirt"
	"github.com/google/uuid"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"libvirt.org/go/libvirtxml"
)

func (cs *ControllerServer) domainDisks(dom libvirt.Domain) ([]libvirtxml.DomainDisk, error) {
	c, err := cs.connectedClient()
	if err != nil {
		return nil, err
	}

	domXML, err := c.DomainGetXMLDesc(dom, 0)
	if err != nil {
		return nil, grpcerr.Internal(err)
	}
	domSpec := &libvirtxml.Domain{}
	if err := domSpec.Unmarshal(domXML); err != nil {
		return nil, grpcerr.Internal(err)
	}

	return domSpec.Devices.Disks, nil
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

func (cs *ControllerServer) isAttachedToDomain(vol *libvirtxml.StorageVolume, dom libvirt.Domain) (disk *libvirtxml.DomainDisk, ok bool, usedDevs []string, err error) {
	disks, err := cs.domainDisks(dom)
	if err != nil {
		return nil, false, nil, err
	}

Loop:
	for _, d := range disks {
		usedDevs = append(usedDevs, d.Target.Dev)
		if d.Source == nil {
			continue Loop
		}

		switch vol.Type {
		case "file":
			file := d.Source.File
			if file == nil {
				continue Loop
			}
			if file.File == vol.Target.Path {
				disk = &d
				break Loop
			}
		case "block":
			block := d.Source.Block
			if block == nil {
				continue Loop
			}
			if block.Dev == vol.Target.Path {
				disk = &d
				break Loop
			}
		}
	}

	if disk == nil {
		return nil, false, usedDevs, nil
	}

	return disk, true, usedDevs, nil
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
