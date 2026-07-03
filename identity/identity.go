// Copyright 2026 Karel Van Hecke
// SPDX-License-Identifier: Apache-2.0

package identity

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

const (
	name    = "com.karelvanhecke.csi.libvirt"
	version = "v0.0.0-dev"
)

var _ csi.IdentityServer = &IdentityServer{}

type IdentityServer struct {
	*csi.UnimplementedIdentityServer
}

func (ids *IdentityServer) GetPluginInfo(context.Context, *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          name,
		VendorVersion: version,
	}, nil
}

func (ids *IdentityServer) GetPluginCapabilities(context.Context, *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_VolumeExpansion_{
					VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
						Type: csi.PluginCapability_VolumeExpansion_ONLINE,
					},
				},
			},
		},
	}, nil
}

func (ids *IdentityServer) Probe(context.Context, *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}
