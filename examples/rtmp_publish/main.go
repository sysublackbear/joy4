package main

import (
	"github.com/nareix/joy4/av/pktque"
	"github.com/nareix/joy4/format"
	"github.com/nareix/joy4/av/avutil"
	"github.com/nareix/joy4/format/rtmp"
)

func init() {
	format.RegisterAll()
}

// as same as: ffmpeg -re -i projectindex.flv -c copy -f flv rtmp://localhost:1936/app/publish
// 实现推流的功能
func main() {
	// Demuxer
	file, _ := avutil.Open("projectindex.flv")
	// Muxer
	conn, _ := rtmp.Dial("rtmp://localhost:1936/app/publish")
	// conn, _ := avutil.Create("rtmp://localhost:1936/app/publish")

	// 由于使用了ffmpeg -re（以本地帧频读数据，主要用于模拟捕获设备），所以这里需要用到Walltime进行修正
	demuxer := &pktque.FilterDemuxer{Demuxer: file, Filter: &pktque.Walltime{}}
	// dst: conn ; src: demuxer
	avutil.CopyFile(conn, demuxer)

	file.Close()
	conn.Close()
}

