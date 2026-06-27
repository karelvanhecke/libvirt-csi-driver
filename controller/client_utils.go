package controller

import (
	"github.com/digitalocean/go-libvirt"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
)

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
