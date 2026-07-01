package controller

import (
	"github.com/digitalocean/go-libvirt"
)

func isVolNotFoundError(err error) bool {
	e, ok := err.(libvirt.Error)
	if !ok || e.Code != uint32(libvirt.ErrNoStorageVol) {
		return false
	}
	return true
}

func isPoolNotFoundError(err error) bool {
	e, ok := err.(libvirt.Error)
	if !ok || e.Code != uint32(libvirt.ErrNoStoragePool) {
		return false
	}
	return true
}

func isResourceBusyError(err error) bool {
	e, ok := err.(libvirt.Error)
	if !ok || e.Code != uint32(libvirt.ErrResourceBusy) {
		return false
	}
	return true
}
