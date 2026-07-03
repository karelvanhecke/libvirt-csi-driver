// Copyright 2026 Karel Van Hecke
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"github.com/digitalocean/go-libvirt"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
)

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
	if isPoolNotFoundError(err) {
		return libvirt.StoragePool{}, grpcerr.InvalidArgument(err)
	}
	return libvirt.StoragePool{}, grpcerr.Internal(err)
}
