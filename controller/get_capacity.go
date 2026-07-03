// Copyright 2026 Karel Van Hecke
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
)

func (cs *ControllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	params := req.GetParameters()

	pool, err := cs.lookupStoragePool(params)
	if err != nil {
		return nil, err
	}

	c, err := cs.connectedClient()
	if err != nil {
		return nil, err
	}

	_, _, _, a, err := c.StoragePoolGetInfo(pool)
	if err != nil {
		return nil, grpcerr.Internal(err)
	}

	return &csi.GetCapacityResponse{
		AvailableCapacity: int64(a),
	}, nil
}
