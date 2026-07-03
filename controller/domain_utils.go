package controller

import (
	"github.com/digitalocean/go-libvirt"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
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
