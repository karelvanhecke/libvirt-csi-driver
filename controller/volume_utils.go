package controller

import (
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
	"libvirt.org/go/libvirtxml"
)

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
