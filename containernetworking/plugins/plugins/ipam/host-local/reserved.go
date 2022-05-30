/*
Copyright 2021.

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
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/plugins/ipam/host-local/backend/allocator"
	"github.com/containernetworking/plugins/plugins/ipam/host-local/backend/disk"
)

const (
	PodReservedIpAnnotationKey = "io.kubernetes.cri/reserved-ip-duration"
)

var defaultDataDir = "/var/lib/cni/networks"

type ReservedInfo struct {
	ContainerId string `json:"containerId"`
	IfName string `json:"ifName"`
	IPConf *current.IPConfig `json:"IPConf"`
	//reserved time duration
	Duration time.Duration `json:"duration"`
	ReleaseTime *time.Time `json:"releaseTime,omitempty"`
}

func isReservedIp(conf *allocator.IPAMConfig) (bool, time.Duration) {
	val,ok := conf.PodAnnotations[PodReservedIpAnnotationKey]
	if !ok {
		return false, 0
	}
	duration,err := strconv.Atoi(val)
	if err!=nil {
		return false, 0
	}
	return true, time.Duration(duration)
}

func getReservedIpFile(args,dir string) string {
	// specify Ip
	var ns, name string
	items := strings.Split(args, ";")
	for _,item := range items {
		if strings.Contains(item, "K8S_POD_NAME=") {
			kv := strings.Split(item, "=")
			name = kv[1]
		}
		if strings.Contains(item, "K8S_POD_NAMESPACE=") {
			kv := strings.Split(item, "=")
			ns = kv[1]
		}
	}
	if dir == "" {
		dir = defaultDataDir
	}
	return path.Join(dir, "reserved", ns, name)
}

func getReservedIp(args, dir string) (*current.IPConfig, error) {
	file := getReservedIpFile(args, dir)
	by, err := ioutil.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var reserved *ReservedInfo
	err = json.Unmarshal(by, &reserved)
	if err!=nil {
		return nil, err
	}
	return reserved.IPConf, nil
}

func allocateReservedIp(args *skel.CmdArgs, ipConf *current.IPConfig) error {
	ipamConf, _, err := allocator.LoadIPAMConfig(args.StdinData, args.Args)
	if err != nil {
		return err
	}
	_,duration := isReservedIp(ipamConf)
	file := getReservedIpFile(args.Args, ipamConf.DataDir)
	// mkdir file
	err = os.MkdirAll(path.Dir(file), os.ModeDir)
	if err != nil {
		return err
	}
	reservedInfo := ReservedInfo{
		ContainerId: args.ContainerID,
		IfName: args.IfName,
		IPConf: ipConf,
		Duration: duration,
		ReleaseTime: nil,
	}
	by,_ := json.Marshal(reservedInfo)
	ioutil.WriteFile(file, by, 0644)
	return err
}

func releaseReservedIp(args, dir string) error {
	file := getReservedIpFile(args, dir)
	by, err := ioutil.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var reserved *ReservedInfo
	err = json.Unmarshal(by, &reserved)
	if err!=nil {
		return nil
	}
	now := time.Now()
	reserved.ReleaseTime = &now
	by,_ = json.Marshal(reserved)
	return ioutil.WriteFile(file, by, 0644)
}

func clearExpiredReservedIp(conf *allocator.IPAMConfig){
	dir := conf.DataDir
	if dir == "" {
		dir = defaultDataDir
	}
	// mkdir file
	err := os.MkdirAll(path.Join(dir, "reserved"), os.ModeDir)
	if err != nil {
		return
	}
	files,err := walkDir(path.Join(dir, "reserved"))
	if err!=nil {
		return
	}

	for _,file :=range files {
		by, err := ioutil.ReadFile(file)
		if err != nil {
			continue
		}
		var reserved *ReservedInfo
		err = json.Unmarshal(by, &reserved)
		if err!=nil {
			continue
		}
		if reserved.ReleaseTime == nil {
			continue
		}
		expiredTime := reserved.ReleaseTime.Add(time.Minute * reserved.Duration)
		if time.Now().After(expiredTime) {
			err = releaseIpInStore(reserved.ContainerId, reserved.IfName, conf)
			if err!=nil {
				continue
			}
			os.Remove(file)
		}
	}
}

func walkDir(dirPth string) (files []string, err error) {
	files = make([]string, 0, 30)
	err = filepath.Walk(dirPth, func(filename string, fi os.FileInfo, err error) error { //遍历目录
		if fi.IsDir() { // 忽略目录
			return nil
		}
		files = append(files, filename)
		return nil
	})

	return files, err
}

func releaseIpInStore(containerId, ifName string, conf *allocator.IPAMConfig) error {
	store, err := disk.New(conf.Name, conf.DataDir)
	if err != nil {
		return err
	}
	defer store.Close()

	// Loop through all ranges, releasing all IPs, even if an error occurs
	for idx, rangeset := range conf.Ranges {
		ipAllocator := allocator.NewIPAllocator(&rangeset, store, idx)
		err = ipAllocator.Release(containerId, ifName)
		if err != nil {
			return err
		}
	}
	return nil
}
