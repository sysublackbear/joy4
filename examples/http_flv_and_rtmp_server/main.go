package main

import (
	"sync"
	"io"
	"net/http"
	"github.com/nareix/joy4/format"
	"github.com/nareix/joy4/av/avutil"
	"github.com/nareix/joy4/av/pubsub"
	"github.com/nareix/joy4/format/rtmp"
	"github.com/nareix/joy4/format/flv"
)

func init() {
	format.RegisterAll()
}

type writeFlusher struct {
	httpflusher http.Flusher
	io.Writer
}

func (self writeFlusher) Flush() error {
	self.httpflusher.Flush()
	return nil
}

// rtmp的Server
func main() {
	/*
	type Server struct {
		Addr          string
		HandlePublish func(*Conn)
		HandlePlay    func(*Conn)
		HandleConn    func(*Conn)
	}
	 */
	server := &rtmp.Server{}

	l := &sync.RWMutex{}
	type Channel struct {
		que *pubsub.Queue
	}
	channels := map[string]*Channel{}

	// 处理播放逻辑
	server.HandlePlay = func(conn *rtmp.Conn) {
		l.RLock()
		ch := channels[conn.URL.Path]
		l.RUnlock()

		if ch != nil {
			cursor := ch.que.Latest()
			// dst:conn, src:cursor
			// 将整个queue的内容拷贝到conn中
			avutil.CopyFile(conn, cursor)
		}
	}

	// 处理推流逻辑
	server.HandlePublish = func(conn *rtmp.Conn) {
		streams, _ := conn.Streams()

		l.Lock()
		ch := channels[conn.URL.Path]
		if ch == nil {
			ch = &Channel{}
			ch.que = pubsub.NewQueue()
			ch.que.WriteHeader(streams)
			channels[conn.URL.Path] = ch
		} else {
			ch = nil
		}
		l.Unlock()
		if ch == nil {
			return
		}

		// ch.que: dst av.PacketWriter; conn: src av.PacketReader
		avutil.CopyPackets(ch.que, conn)

		l.Lock()
		delete(channels, conn.URL.Path)
		l.Unlock()
		ch.que.Close()
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		l.RLock()
		ch := channels[r.URL.Path]
		l.RUnlock()

		if ch != nil {
			w.Header().Set("Content-Type", "video/x-flv")
			w.Header().Set("Transfer-Encoding", "chunked")		
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(200)
			flusher := w.(http.Flusher)
			flusher.Flush()

			muxer := flv.NewMuxerWriteFlusher(writeFlusher{httpflusher: flusher, Writer: w})
			cursor := ch.que.Latest()

			avutil.CopyFile(muxer, cursor)
		} else {
			http.NotFound(w, r)
		}
	})

	go http.ListenAndServe(":8089", nil)

	server.ListenAndServe()

	// ffmpeg -re -i movie.flv -c copy -f flv rtmp://localhost/movie
	// ffmpeg -f avfoundation -i "0:0" .... -f flv rtmp://localhost/screen
	// ffplay http://localhost:8089/movie
	// ffplay http://localhost:8089/screen
}
