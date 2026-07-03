// Copyright 2026 Karel Van Hecke
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/digitalocean/go-libvirt"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
)

type ControllerServer struct {
	*csi.UnimplementedControllerServer

	client      *libvirt.Libvirt
	defaultPool string

	defaultCapacity uint64

	fsTypes     []string
	accessModes []csi.VolumeCapability_AccessMode_Mode

	caps []*csi.ControllerServiceCapability
}

func NewControllerServer(client *libvirt.Libvirt, defaultPool string) *ControllerServer {
	cs := &ControllerServer{
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

	caps := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
		csi.ControllerServiceCapability_RPC_GET_CAPACITY,
		csi.ControllerServiceCapability_RPC_PUBLISH_READONLY,
		csi.ControllerServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
	}
	for _, cap := range caps {
		cs.caps = append(cs.caps, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		})
	}

	return cs
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

func (cs *ControllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.caps,
	}, nil
}
