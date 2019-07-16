package avconv

import (
	"fmt"
	"io"
	"time"
	"github.com/nareix/joy4/av/avutil"
	"github.com/nareix/joy4/av"
	"github.com/nareix/joy4/av/pktque"
	"github.com/nareix/joy4/av/transcode"
)

var Debug bool

type Option struct {
	Transcode bool
	Args []string
}

type Options struct {
	OutputCodecTypes []av.CodecType
}

type Demuxer struct {
	transdemux *transcode.Demuxer
	streams []av.CodecData
	Options
	Demuxer av.Demuxer
}

func (self *Demuxer) Close() (err error) {
	if self.transdemux != nil {
		return self.transdemux.Close()
	}
	return
}

func (self *Demuxer) Streams() (streams []av.CodecData, err error) {
	if err = self.prepare(); err != nil {
		return
	}
	// todo: streams成员由谁去赋值?
	streams = self.streams  // 直接读取streams成员
	return
}

func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
	if err = self.prepare(); err != nil {
		return
	}
	return self.transdemux.ReadPacket()  // 直接调用transcoder的ReadPacket方法
}

func (self *Demuxer) prepare() (err error) {
	if self.transdemux != nil {  // trancoder已初始化
		return
	}

	/*
	var streams []av.CodecData
	if streams, err = self.Demuxer.Streams(); err != nil {
		return
	}
	*/

	// 初始化transcoder
	// supports: 支持哪些编码格式
	supports := self.Options.OutputCodecTypes   // []av.CodecType

	transopts := transcode.Options{}
	transopts.FindAudioDecoderEncoder = func(codec av.AudioCodecData, i int) (ok bool, dec av.AudioDecoder, enc av.AudioEncoder, err error) {
		if len(supports) == 0 {
			return
		}

		support := false
		for _, typ := range supports {
			if typ == codec.Type() {
				support = true  // codec的Type是已经在需要支持的编码格式列表里面，实则返回成功
			}
		}

		if support {
			return
		}
		ok = true

		var enctype av.CodecType  // 待转码的类型
		for _, typ:= range supports {
			if typ.IsAudio() {  // 属于音频信号
				// 获取编码器
				if enc, _ = avutil.DefaultHandlers.NewAudioEncoder(typ); enc != nil {
					enctype = typ
					break
				}
			}
		}
		if enc == nil {
			err = fmt.Errorf("avconv: convert %s->%s failed", codec.Type(), enctype)
			return
		}

		// TODO: support per stream option
		// enc.SetSampleRate ...

		// 获取解码器
		if dec, err = avutil.DefaultHandlers.NewAudioDecoder(codec); err != nil {
			err = fmt.Errorf("avconv: decode %s failed", codec.Type())
			return
		}

		return
	}

	// 初始化transdemux
	self.transdemux = &transcode.Demuxer{
		Options: transopts,
		Demuxer: self.Demuxer,
	}
	if self.streams, err = self.transdemux.Streams(); err != nil {
		return
	}

	return
}


// -i : 要处理的视频文件路径
// -v : 仅做打印
// -t
func ConvertCmdline(args []string) (err error) {
	output := ""
	input := ""
	flagi := false
	flagv := false
	flagt := false
	flagre := false
	duration := time.Duration(0)
	options := Options{}

	for _, arg := range args {
		switch arg {
		case "-i":
			flagi = true

		case "-v":
			flagv = true

		case "-t":
			flagt = true

		case "-re":
			flagre = true

		default:
			switch {
			case flagi:
				flagi = false
				input = arg

			case flagt:
				flagt = false
				var f float64
				fmt.Sscanf(arg, "%f", &f)
				duration = time.Duration(f*float64(time.Second))

			default:
				output = arg
			}
		}
	}

	if input == "" {
		err = fmt.Errorf("avconv: input file not specified")
		return
	}

	if output == "" {
		err = fmt.Errorf("avconv: output file not specified")
		return
	}

	var demuxer av.DemuxCloser
	var muxer av.MuxCloser

	// 获取分离器
	if demuxer, err = avutil.Open(input); err != nil {
		return
	}
	defer demuxer.Close()

	// 获取合并器
	var handler avutil.RegisterHandler
	if handler, muxer, err = avutil.DefaultHandlers.FindCreate(output); err != nil {
		return
	}
	defer muxer.Close()

	options.OutputCodecTypes = handler.CodecTypes

	convdemux := &Demuxer{
		Options: options,
		Demuxer: demuxer,
	}
	defer convdemux.Close()

	var streams []av.CodecData
	if streams, err = demuxer.Streams(); err != nil {
		return
	}

	var convstreams []av.CodecData
	if convstreams, err = convdemux.Streams(); err != nil {
		return
	}

	if flagv {
		for _, stream := range streams {
			fmt.Print(stream.Type(), " ")
		}
		fmt.Print("-> ")
		for _, stream := range convstreams {
			fmt.Print(stream.Type(), " ")
		}
		fmt.Println()
	}

	if err = muxer.WriteHeader(convstreams); err != nil {
		return
	}

	filters := pktque.Filters{}
	if flagre {
		filters = append(filters, &pktque.Walltime{})
	}
	filterdemux := &pktque.FilterDemuxer{
		Demuxer: convdemux,
		Filter: filters,
	}

	for {
		var pkt av.Packet
		if pkt, err = filterdemux.ReadPacket(); err != nil {
			if err == io.EOF {
				err = nil
				break
			}
			return
		}
		if flagv {
			fmt.Println(pkt.Idx, pkt.Time, len(pkt.Data), pkt.IsKeyFrame)
		}
		// 大于时长，则退出
		if duration != 0 && pkt.Time > duration {
			break
		}
		if err = muxer.WritePacket(pkt); err != nil {
			return
		}
	}

	if err = muxer.WriteTrailer(); err != nil {
		return
	}

	return
}

// finish