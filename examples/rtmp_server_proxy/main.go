package main

import (
	"fmt"
	"strings"
	"github.com/nareix/joy4/format"
	"github.com/nareix/joy4/av/avutil"
	"github.com/nareix/joy4/format/rtmp"
)

func init() {
	format.RegisterAll()
}

func main() {
	server := &rtmp.Server{}

	// 这个server的逻辑相当于一个代理，把URL改写成另外一个URL进行播放
	server.HandlePlay = func(conn *rtmp.Conn) {
		// conn.URL.Path = "localhost/rtsp/192.168.1.1/camera1"
		segs := strings.Split(conn.URL.Path, "/")
		// url = "rtsp://192.168.1.1/camera1"
		url := fmt.Sprintf("%s://%s", segs[1], strings.Join(segs[2:], "/"))
		src, _ := avutil.Open(url)
		avutil.CopyFile(conn, src)
	}

	server.ListenAndServe()

	// ffplay rtmp://localhost/rtsp/192.168.1.1/camera1
	// ffplay rtmp://localhost/rtmp/live.hkstv.hk.lxdns.com/live/hks
}
