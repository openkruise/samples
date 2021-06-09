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
	"fmt"
	"io"
	"io/ioutil"
	"k8s.io/klog"
	"net"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"time"
)

var (
	Version = ""
)

const (
	unixSocket = "/dev/shm/migrate.sock"

	// sidecar container version in container env(SIDECARSET_VERSION)
	SidecarSetVersionEnvKey = "SIDECARSET_VERSION"
	// container version env in the other sidecar container of the same hotupgrade sidecar(SIDECARSET_VERSION_ALT)
	SidecarSetVersionAltEnvKey = "SIDECARSET_VERSION_ALT"
)

func main() {
	// empty container
	if Version == "empty" {
		ch := make(chan struct{})
		<-ch
	}

	isHotUpgrading, newer := isHotUpgradeProcess()
	var tcpLn *net.TCPListener
	var err error
	// in hot upgrade process
	if isHotUpgrading {
		klog.Infof("start hot upgrade")
		// Abnormal exit scenario of old version sidecar during hot upgrade
		if !newer {
			klog.Infof("older sidecar don't listen, and exit 0")
			time.Sleep(time.Minute)
			os.Exit(0)
		}
		tcpLn, err = triggerOldSidecarMigrate()
		if err != nil {
			klog.Errorf("migrate older sidecar ListenFd failed, and will Listen new")
		}
	}
	if tcpLn == nil {
		tcpAddr, err := net.ResolveTCPAddr("tcp", ":9091")
		if err != nil {
			klog.Errorf(err.Error())
			return
		}
		tcpLn, err = net.ListenTCP("tcp", tcpAddr)
		if err != nil {
			klog.Errorf(err.Error())
			return
		}
	}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/migrate", func(writer http.ResponseWriter, request *http.Request) {
		err := migrateListenFd(tcpLn)
		var writerStr string
		if err == nil {
			writerStr = "success"
			//The current old version of sidecar has sent ListenFD to the new version via UDS, so a graceful exit needs to be completed at this point, i.e.
			// 1. stop accepting new requests
			// 2. for long connect requests, send a disconnect flag and close the current connection,
			//    e.g. http2 GOAWAY indicates that the server side will disconnect the link, which will trigger the client to retry
			if err = tcpLn.Close(); err != nil {
				klog.Errorf("close ListenFD failed: %s", err.Error())
			}
		} else {
			writerStr = "failed"
		}
		_, err = io.WriteString(writer, writerStr)
		if err != nil {
			klog.Errorf("response /migrate failed: %s", err.Error())
		}
	})
	serveMux.HandleFunc("/serve", func(writer http.ResponseWriter, request *http.Request) {
		time.Sleep(time.Millisecond * 10)
		_, err = io.WriteString(writer, fmt.Sprintf("This is version(%s) sidecar", Version))
		if err != nil {
			klog.Errorf("response /serve failed: %s", err.Error())
		}
	})

	serveErr := make(chan error)
	go func() {
		klog.Infof("Listen(%s) and serve(version=%s)", tcpLn.Addr(), Version)
		err = http.Serve(tcpLn, serveMux)
		if err != nil {
			serveErr <- err
		}
	}()

	ticker := time.NewTicker(time.Second * 5)
	select {
	case <-ticker.C:
		file, err := os.OpenFile("/result", os.O_CREATE|os.O_RDWR|os.O_APPEND, os.ModeAppend|os.ModePerm)
		if err != nil {
			klog.Errorf("start failed: %s", err.Error())
			os.Exit(1)
		}
		_, err = file.WriteString("success")
		if err != nil {
			klog.Errorf("start failed: %s", err.Error())
			os.Exit(1)
		}
		if err := file.Close(); err != nil {
			klog.Errorf("close file failed: %s", err.Error())
		}
		klog.Infof("serve success")
		ch := make(chan struct{})
		<-ch
	case err := <-serveErr:
		klog.Errorf("Listen(%s) and serve failed: %s", tcpLn.Addr(), err.Error())
		os.Exit(1)
	}

	// migration done, then waiting for 10s and exit 0
	klog.Infof("close Listen fd, and waiting 10s")
	time.Sleep(time.Second * 10)
	os.Exit(0)
}

// return two parameters:
// 1. isHotUpgrading(bool) indicates whether it is hot upgrade process
// 2. when isHotUpgrading=true, the current sidecar is newer or older
//    true: newer; false: older
func isHotUpgradeProcess() (bool, bool) {
	// sidecar container version in container env(SIDECARSET_VERSION)
	version := os.Getenv(SidecarSetVersionEnvKey)
	// container version env in the other sidecar container of the same hotupgrade sidecar(SIDECARSET_VERSION_ALT)
	versionAlt := os.Getenv(SidecarSetVersionAltEnvKey)
	// is not in hot upgrade process
	if versionAlt == "" || versionAlt == "0" {
		return false, false
	}

	versionInt, err := strconv.Atoi(version)
	if err != nil {
		klog.Errorf("strconv env(%s) failed: %s", SidecarSetVersionEnvKey, err.Error())
		return false, false
	}
	versionAltInt, err := strconv.Atoi(versionAlt)
	if err != nil {
		klog.Errorf("strconv env(%s) failed: %s", SidecarSetVersionAltEnvKey, err.Error())
		return false, false
	}

	return true, versionInt > versionAltInt
}

func migrateListenFd(tcpLn *net.TCPListener) error {
	klog.Infof("start migrate old sidecar Listener to newer sidecar")
	f, err := tcpLn.File()
	if err != nil {
		klog.Errorf(err.Error())
		return err
	}
	fdnum := f.Fd()
	klog.Infof("%b %b %b %b", byte(fdnum), byte(fdnum>>8), byte(fdnum>>16), byte(fdnum>>24))
	klog.Infof("ready to send fd: %d", fdnum)
	data := syscall.UnixRights(int(fdnum))
	raddr, err := net.ResolveUnixAddr("unix", unixSocket)
	if err != nil {
		klog.Errorf(err.Error())
		return err
	}
	uds, err := net.DialUnix("unix", nil, raddr)
	if err != nil {
		klog.Errorf(err.Error())
		return err
	}
	n, oobn, err := uds.WriteMsgUnix(nil, data, nil)
	if err != nil {
		klog.Errorf(err.Error())
		return err
	}
	klog.Infof("WriteMsgUnix = %d, %d; want 1, %d", n, oobn, len(data))
	// close current listen fd
	tcpLn.Close()
	klog.Infof("close ListenFd, and waiting for current handler request")
	return nil
}

func triggerOldSidecarMigrate() (*net.TCPListener, error) {
	syscall.Unlink(unixSocket)
	addr, err := net.ResolveUnixAddr("unix", unixSocket)
	if err != nil {
		return nil, err
	}
	unixLn, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, err
	}
	defer unixLn.Close()

	ch := make(chan *net.TCPListener)
	go func() {
		tcpLn, err := receiveListenFd(unixLn)
		if err != nil {
			klog.Errorf(err.Error())
		}
		ch <- tcpLn
	}()

	klog.Infof("request migration: http://127.0.0.1:9091/migrate")
	// request older sidecar to migrate Listen Fd
	resp, err := http.Get("http://127.0.0.1:9091/migrate")
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("resp Status Code %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// migration failed
	if string(body) != "success" {
		return nil, fmt.Errorf("migration failed")
	}

	tcpLn := <-ch
	if tcpLn == nil {
		return nil, fmt.Errorf("migration failed")
	}
	return tcpLn, nil
}

func receiveListenFd(unixLn *net.UnixListener) (*net.TCPListener, error) {
	klog.Infof("waiting for ListenFd from unix socks")
	conn, err := unixLn.AcceptUnix()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	buf := make([]byte, 32)
	oob := make([]byte, 32)
	_, oobn, _, _, err := conn.ReadMsgUnix(buf, oob)
	if err != nil {
		return nil, err
	}
	scms, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		panic(err)
	}
	if len(scms) > 0 {
		fds, err := syscall.ParseUnixRights(&(scms[0]))
		if err != nil {
			return nil, err
		}
		klog.Infof("parse %d fds: %v \n", len(fds), fds)
		f := os.NewFile(uintptr(fds[0]), "")
		ln, err := net.FileListener(f)
		if err != nil {
			return nil, err
		}
		tcpLn, ok := ln.(*net.TCPListener)
		if !ok {
			return nil, fmt.Errorf("Not TCPListener")
		}
		return tcpLn, nil
	}

	return nil, fmt.Errorf("Not receive TCPListener")
}
