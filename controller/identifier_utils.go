package controller

import (
	"crypto/rand"
	"encoding/hex"
	"slices"

	"github.com/digitalocean/go-libvirt"
	grpcerr "github.com/karelvanhecke/libvirt-csi-driver/pkg/errors"
)

func volumeId(vol libvirt.StorageVol) string {
	return vol.Key
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

	return "", grpcerr.Internal(ErrGenerateDeviceID)
}

func generateWWN() (string, error) {
	randBytes := make([]byte, 8)
	if _, err := rand.Read(randBytes); err != nil {
		return "", grpcerr.Internal(err)
	}
	return hex.EncodeToString(randBytes), nil
}
