/*
Copyright 2021 The Kruise Authors.

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

package main

import (
	"io/ioutil"
	"k8s.io/klog"
	"net/http"
	"time"
)

func main() {
	for {
		resp, err := http.Get("http://127.0.0.1:9091/serve")
		if err != nil {
			klog.Errorf("request sidecar(http://127.0.0.1:9091/serve) failed: %s", err.Error())
			return
		}

		by, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			klog.Errorf("read resp body failed: %s", err.Error())
			return
		}
		klog.Infof("request sidecar server success, and response(body=%s)", string(by))
		time.Sleep(time.Millisecond * 100)
	}
}
