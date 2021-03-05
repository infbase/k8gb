/*
Copyright 2021 Absa Group Limited

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidDig(t *testing.T) {
	// arrange
	if !connected() {
		t.Skipf("no connectivity, skipping")
	}
	edgeDNSServer := "8.8.8.8"
	fqdn := "google.com"
	// act
	result, err := Dig(edgeDNSServer, fqdn)
	// assert
	assert.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.NotEmpty(t, result[0])
}

func TestEmptyFQDNButValidEdgeDNS(t *testing.T) {
	// arrange
	if !connected() {
		t.Skipf("no connectivity, skipping")
	}
	edgeDNSServer := "8.8.8.8"
	fqdn := ""
	// act
	result, err := Dig(edgeDNSServer, fqdn)
	// assert
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestEmptyEdgeDNS(t *testing.T) {
	// arrange
	edgeDNSServer := ""
	fqdn := "whatever"
	// act
	result, err := Dig(edgeDNSServer, fqdn)
	// assert
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestValidEdgeDNSButNonExistingFQDN(t *testing.T) {
	// arrange
	edgeDNSServer := "localhost"
	fqdn := "some-valid-ip-fqdn-123"
	// act
	result, err := Dig(edgeDNSServer, fqdn)
	// assert
	assert.Error(t, err)
	assert.Nil(t, result)
}

func connected() (ok bool) {
	res, err := http.Get("http://google.com")
	if err != nil {
		return false
	}
	return res.Body.Close() == nil
}