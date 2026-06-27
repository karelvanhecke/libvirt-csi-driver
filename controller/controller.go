package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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

var (
	ErrVolumeMustBeReadOnly = errors.New("volume must be read only")
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

func (cs *ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	c, err := cs.connectedClient()
	if err != nil {
		return nil, err
	}

	if _, err := c.StorageVolLookupByKey(req.GetVolumeId()); err != nil {
		if isVolNotFoundError(err) {
			return nil, grpcerr.NotFound(err)
		}
		return nil, grpcerr.Internal(err)
	}

	resp := &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{},
	}

	for _, cap := range req.VolumeCapabilities {
		if err := cs.verifyVolumeCapability(cap); err != nil {
			return nil, grpcerr.InvalidArgument(err)
		}
		resp.Confirmed.VolumeCapabilities = append(resp.Confirmed.VolumeCapabilities, cap)
	}

	return resp, nil
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

// Verify volume capability is supported.
func (cs *ControllerServer) verifyVolumeCapability(cap *csi.VolumeCapability) error {
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
	return nil
}

// Make sure all requested volume capabilities are supported.
func (cs *ControllerServer) verifyVolumeCapabilities(caps []*csi.VolumeCapability) error {
	for _, cap := range caps {
		if err := cs.verifyVolumeCapability(cap); err != nil {
			return err
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

	e, ok := err.(libvirt.Error)
	if !ok || e.Code != uint32(libvirt.ErrResourceBusy) {
		return grpcerr.Internal(err)
	}

	doms, _, err := c.ConnectListAllDomains(1, libvirt.ConnectListDomainsActive)
	if err != nil {
		return grpcerr.Internal(err)
	}

	for _, dom := range doms {
		_, ok, _, err := cs.isAttachedToDomain(vol, dom)
		if err != nil {
			return grpcerr.Internal(err)
		}

		if ok {
			id, err := uuid.ParseBytes(dom.UUID[:])
			if err != nil {
				return grpcerr.Internal(err)
			}

			return status.Error(codes.FailedPrecondition, id.String())
		}
	}

	return grpcerr.Internal(e)
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

func isVolNotFoundError(err error) bool {
	e, ok := err.(libvirt.Error)
	if !ok || e.Code != uint32(libvirt.ErrNoStorageVol) {
		return false
	}
	return true
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

	// In theory this will never be reached.
	// The attached target limit is lower than the amount of possible ID's
	return "", grpcerr.Internal(errors.New("could not generate device ID"))
}

func generateWWN() (string, error) {
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return "", grpcerr.Internal(err)
	}
	return hex.EncodeToString(randBytes), nil
}
