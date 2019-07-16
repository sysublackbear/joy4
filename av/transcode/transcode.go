
// Package transcoder implements Transcoder based on Muxer/Demuxer and AudioEncoder/AudioDecoder interface.
package transcode

import (
	"fmt"
	"time"
	"github.com/nareix/joy4/av"
	"github.com/nareix/joy4/av/pktque"
)

var Debug bool

type tStream struct {
	codec av.CodecData
	timeline *pktque.Timeline
	aencodec, adecodec av.AudioCodecData
	aenc av.AudioEncoder
	adec av.AudioDecoder
}

type Options struct {
	// check if transcode is needed, and create the AudioDecoder and AudioEncoder.
	FindAudioDecoderEncoder func(codec av.AudioCodecData, i int) (
		need bool, dec av.AudioDecoder, enc av.AudioEncoder, err error,
	)
}

// 转码器
type Transcoder struct {
	streams                 []*tStream
}

func NewTranscoder(streams []av.CodecData, options Options) (_self *Transcoder, err error) {
	self := &Transcoder{}
	self.streams = []*tStream{}

	for i, stream := range streams {
		ts := &tStream{codec: stream}
		if stream.Type().IsAudio() {  // 如果是音频
			if options.FindAudioDecoderEncoder != nil {
				// 获取编码器和解码器
				var ok bool
				var enc av.AudioEncoder
				var dec av.AudioDecoder
				ok, dec, enc, err = options.FindAudioDecoderEncoder(stream.(av.AudioCodecData), i)
				if ok {
					if err != nil {
						return
					}
					ts.timeline = &pktque.Timeline{}
					if ts.codec, err = enc.CodecData(); err != nil {
						return
					}
					ts.aencodec = ts.codec.(av.AudioCodecData)  // 获取描述
					ts.adecodec = stream.(av.AudioCodecData)  // 获取描述
					ts.aenc = enc
					ts.adec = dec
				}
			}
		}
		self.streams = append(self.streams, ts)
	}

	_self = self
	return
}

// 先解码再编码
// 1.调用解码器进行解码
// 2.把音频的时长加入到timeline里面
// 3.调用编码器进行编码，由于解码器和编码器不一样，所以达到了转码的目的
// 4.把音频时长加入到回包的packet里面，给packet一个时长
// 输入一个av.Packet，返回一个av.Packet列表
func (self *tStream) audioDecodeAndEncode(inpkt av.Packet) (outpkts []av.Packet, err error) {
	var dur time.Duration
	var frame av.AudioFrame
	var ok bool
	// 解码(bool, AudioFrame, error)
	// 调用解码器进行解码
	if ok, frame, err = self.adec.Decode(inpkt.Data); err != nil {
		return
	}
	if !ok {
		return
	}

	// 获取压缩时长?
	// get audio compressed packet duration
	if dur, err = self.adecodec.PacketDuration(inpkt.Data); err != nil {
		err = fmt.Errorf("transcode: PacketDuration() failed for input stream #%d", inpkt.Idx)
		return
	}

	if Debug {
		fmt.Println("transcode: push", inpkt.Time, dur)
	}
	// 加到timeline里面
	self.timeline.Push(inpkt.Time, dur)

	var _outpkts [][]byte
	// 编码回去
	// ([][]byte, error)
	// todo: 这里解码和编码的协议不一样?
	if _outpkts, err = self.aenc.Encode(frame); err != nil {
		return
	}
	for _, _outpkt := range _outpkts {
		// get audio compressed packet duration
		if dur, err = self.aencodec.PacketDuration(_outpkt); err != nil {
			err = fmt.Errorf("transcode: PacketDuration() failed for output stream #%d", inpkt.Idx)
			return
		}
		outpkt := av.Packet{Idx: inpkt.Idx, Data: _outpkt}
		outpkt.Time = self.timeline.Pop(dur)

		if Debug {
			// 转码完成
			fmt.Println("transcode: pop", outpkt.Time, dur)
		}

		outpkts = append(outpkts, outpkt)
	}

	return
}

// Do the transcode.
//
// 在音频转码中，一个Packet可能会转码成多个Packet
// 包的时间会自动的调整
// In audio transcoding one Packet may transcode into many Packets
// packet time will be adjusted automatically.
func (self *Transcoder) Do(pkt av.Packet) (out []av.Packet, err error) {
	stream := self.streams[pkt.Idx]
	if stream.aenc != nil && stream.adec != nil {
		if out, err = stream.audioDecodeAndEncode(pkt); err != nil {
			return
		}
	} else {
		// 编码器或者解码器为空，默认不转码
		out = append(out, pkt)
	}
	return
}

// Get CodecDatas after transcoding.
// 获取转码过后的CodecData（元数据）
func (self *Transcoder) Streams() (streams []av.CodecData, err error) {
	for _, stream := range self.streams {
		streams = append(streams, stream.codec)
	}
	return
}

// Close transcoder, close related encoder and decoders.
// 关闭流
func (self *Transcoder) Close() (err error) {
	for _, stream := range self.streams {
		if stream.aenc != nil {
			stream.aenc.Close()
			stream.aenc = nil
		}
		if stream.adec != nil {
			stream.adec.Close()
			stream.adec = nil
		}
	}
	self.streams = nil
	return
}

// Wrap transcoder and origin Muxer into new Muxer.
// Write to new Muxer will do transcoding automatically.
// 音视频复用器
type Muxer struct {
	av.Muxer // origin Muxer
	Options // transcode options
	transcoder *Transcoder
}

func (self *Muxer) WriteHeader(streams []av.CodecData) (err error) {
	// 初始化transcoder成员
	// 这里实现得上下不对齐
	if self.transcoder, err = NewTranscoder(streams, self.Options); err != nil {
		return
	}
	var newstreams []av.CodecData
	if newstreams, err = self.transcoder.Streams(); err != nil {
		return
	}
	// 写入编码的头部(newstreams-[]av.CodecData)
	// 相当于将函数入参streams写入?
	if err = self.Muxer.WriteHeader(newstreams); err != nil {
		return
	}
	return
}

func (self *Muxer) WritePacket(pkt av.Packet) (err error) {
	var outpkts []av.Packet
	// 转码，先解码再重新编码
	if outpkts, err = self.transcoder.Do(pkt); err != nil {
		return
	}
	for _, pkt := range outpkts {
		if err = self.Muxer.WritePacket(pkt); err != nil {
			return
		}
	}
	return
}

func (self *Muxer) Close() (err error) {
	if self.transcoder != nil {
		return self.transcoder.Close()
	}
	return
}

// Wrap transcoder and origin Demuxer into new Demuxer.
// Read this Demuxer will do transcoding automatically.
// 音视频分离器
type Demuxer struct {
	av.Demuxer
	Options
	transcoder *Transcoder
	outpkts []av.Packet
}

func (self *Demuxer) prepare() (err error) {
	// transcoder已经指定了就无需重新创建
	if self.transcoder == nil {
		var streams []av.CodecData
		if streams, err = self.Demuxer.Streams(); err != nil {
			return
		}
		if self.transcoder, err = NewTranscoder(streams, self.Options); err != nil {
			return
		}
	}
	return
}

func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
	// 相当于上面: if self.transcoder, err = NewTranscoder(streams, self.Options); err != nil {
	if err = self.prepare(); err != nil {
		return
	}
	for {
		// outpkts已经有数据，直接读取第一个包，不需要做转码操作（重复读）
		if len(self.outpkts) > 0 {
			pkt = self.outpkts[0]
			self.outpkts = self.outpkts[1:]
			return
		}
		var rpkt av.Packet
		// 从分离器读取一个包
		if rpkt, err = self.Demuxer.ReadPacket(); err != nil {
			return
		}
		// 进行转码
		if self.outpkts, err = self.transcoder.Do(rpkt); err != nil {
			return
		}
	}
	return
}

func (self *Demuxer) Streams() (streams []av.CodecData, err error) {
	if err = self.prepare(); err != nil {
		return
	}
	return self.transcoder.Streams()
}

func (self *Demuxer) Close() (err error) {
	if self.transcoder != nil {
		return self.transcoder.Close()
	}
	return
}

// finish
