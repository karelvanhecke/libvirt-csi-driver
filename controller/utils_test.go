package controller

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"testing"

	"github.com/digitalocean/go-libvirt"
)

const (
	testPool        = "default-pool"
	testVol         = "default-vol"
	testVolCapacity = 1000000
)

func testClient(t *testing.T) *libvirt.Libvirt {
	wd, err := os.Getwd()
	if err != nil {
		setupFail(t, err)
	}

	uri, err := url.Parse(fmt.Sprintf("test://%s/%s", wd, "testdata/driver.xml"))
	if err != nil {
		setupFail(t, err)
	}

	l, err := libvirt.ConnectToURI(uri)
	if err != nil {
		setupFail(t, err)
	}
	return l
}

func setupFail(t *testing.T, err error) {
	log.Fatalf("Failed to setup test %s: %s", t.Name(), err)
}
