package controller

import (
	"testing"

	"github.com/digitalocean/go-libvirt"
	"github.com/digitalocean/go-libvirt/socket/dialers"
	"libvirt.org/go/libvirtxml"
)

type testEnv struct {
	*testing.T
	client *libvirt.Libvirt
	pool   libvirt.StoragePool
}

func newTestEnv(t *testing.T, opts ...testEnvOption) *testEnv {
	te := &testEnv{
		T:      t,
		client: libvirt.NewWithDialer(dialers.NewLocal()),
	}

	if err := te.client.Connect(); err != nil {
		t.Fatal(err)
	}

	poolXML, err := (&libvirtxml.StoragePool{
		Type: "dir",
		Name: t.Name(),
		Target: &libvirtxml.StoragePoolTarget{
			Path: t.TempDir(),
		},
	}).Marshal()
	if err != nil {
		t.Fatal(err)
	}

	te.pool, err = te.client.StoragePoolCreateXML(poolXML, libvirt.StoragePoolCreateNormal)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := te.client.StoragePoolDestroy(te.pool); err != nil {
			t.Log(err)
		}
		if err := te.client.Disconnect(); err != nil {
			t.Log(err)
		}
	})

	for _, o := range opts {
		o(te)
	}

	return te
}

func (te *testEnv) poolName() string {
	return te.pool.Name
}

func (te *testEnv) volumeExists(id string) bool {
	_, err := te.client.StorageVolLookupByKey(id)
	if err == nil {
		return true
	}
	if !isVolNotFoundError(err) {
		te.Fatal(err)
	}
	return false
}

func (te *testEnv) volumeCapacity(id string) int64 {
	v, err := te.client.StorageVolLookupByKey(id)
	if err != nil {
		te.Fatal(err)
	}
	_, cap, _, err := te.client.StorageVolGetInfo(v)
	if err != nil {
		te.Fatal(err)
	}

	return int64(cap)
}

func (te *testEnv) volumeID(name string) string {
	v, err := te.client.StorageVolLookupByName(te.pool, name)
	if err != nil {
		te.Fatal(err)
	}
	return v.Key
}

type testEnvOption func(*testEnv)

func withVolumes(volumes ...*libvirtxml.StorageVolume) testEnvOption {
	return func(te *testEnv) {
		for _, v := range volumes {
			xml, err := v.Marshal()
			if err != nil {
				te.Fatal(err)
			}
			if _, err := te.client.StorageVolCreateXML(te.pool, xml, 0); err != nil {
				te.Fatal(err)
			}
		}
	}
}
