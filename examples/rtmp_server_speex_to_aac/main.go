package main

import (
	"github.com/nareix/joy4/av"
	"github.com/nareix/joy4/av/transcode"
	"github.com/nareix/joy4/format"
	"github.com/nareix/joy4/av/avutil"
	"github.com/nareix/joy4/format/rtmp"
	"github.com/nareix/joy4/cgo/ffmpeg"
)

// need ffmpeg with libspeex and libfdkaac installed
// 
// open http://www.wowza.com/resources/4.4.1/examples/WebcamRecording/FlashRTMPPlayer11/player.html
// click connect and recored
// input camera H264/SPEEX will converted H264/AAC and saved in out.ts

func init() {
	format.RegisterAll()
}

func main() {
	server := &rtmp.Server{}

	server.HandlePublish = func(conn *rtmp.Conn) {
		file, _ := avutil.Create("out.ts")

		// 获取转码器（解码+编码），细节暂时不需要了解，调用ffmpeg框架，获取可用的转码器findcodec
		findcodec := func(stream av.AudioCodecData, i int) (need bool, dec av.AudioDecoder, enc av.AudioEncoder, err error) {
			need = true
			dec, _ = ffmpeg.NewAudioDecoder(stream)
			enc, _ = ffmpeg.NewAudioEncoderByName("libfdk_aac")
			enc.SetSampleRate(48000)
			enc.SetChannelLayout(av.CH_STEREO)
			return
		}

		trans := &transcode.Demuxer{
			Options: transcode.Options{
				FindAudioDecoderEncoder: findcodec,
			},
			Demuxer: conn,
		}

		avutil.CopyFile(file, trans)
	}

	server.ListenAndServe()
}
