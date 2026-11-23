package docker

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// maxLogLineBytes 是单行日志的上限，超出部分截断，防止异常长行耗尽内存。
const maxLogLineBytes = 64 << 10

// LogLine 是一行容器日志。
type LogLine struct {
	Stream string `json:"stream"` // stdout 或 stderr
	Time   string `json:"time,omitempty"`
	Text   string `json:"line"`
}

// DemuxReader 从 Docker logs 流按行读取日志。
// 非 TTY 容器的流由 8 字节 multiplex header（streamType + 3 字节保留 + 4 字节大端长度）加
// payload 组成，逐帧剥离；TTY 容器为裸流直通。timestamps=1 时每行带 RFC3339Nano 前缀。
type DemuxReader struct {
	raw io.ReadCloser
	tty bool

	eof bool
	// 按 stream 累积尚未满一行的数据（行可能跨帧）。
	pending [3][]byte // 1=stdout 2=stderr，0 留给 tty 直通
	ready   []LogLine // 已切好、按到达顺序等待消费的行
}

// NewDemuxReader 包装 logs 原始流；tty 为 true 时按裸流处理。
func NewDemuxReader(raw io.ReadCloser, tty bool) *DemuxReader {
	return &DemuxReader{raw: raw, tty: tty}
}

// Close 关闭底层流（即中止 Docker logs 请求）。
func (d *DemuxReader) Close() error {
	return d.raw.Close()
}

// ReadLine 返回下一行日志；流正常结束返回 io.EOF；帧截断等异常返回其他错误。
func (d *DemuxReader) ReadLine() (LogLine, error) {
	for {
		if len(d.ready) > 0 {
			line := d.ready[0]
			d.ready = d.ready[1:]
			return line, nil
		}
		if d.eof {
			// 收尾：flush 各 stream 缓冲中未带换行的残余数据。
			for stream := byte(0); stream < 3; stream++ {
				if len(d.pending[stream]) > 0 {
					line := makeLogLine(stream, string(d.pending[stream]))
					d.pending[stream] = nil
					return line, nil
				}
			}
			return LogLine{}, io.EOF
		}
		if err := d.fill(); err != nil {
			switch err {
			case io.EOF:
				d.eof = true
			case io.ErrUnexpectedEOF:
				// 半帧：头或 payload 未收满即结束，视为异常截断。
				return LogLine{}, fmt.Errorf("容器日志流截断：%w", err)
			default:
				return LogLine{}, err
			}
		}
	}
}

// fill 读取并解析下一帧（或裸流下一段），把完整行推入 ready。
func (d *DemuxReader) fill() error {
	if d.tty {
		chunk := make([]byte, 32<<10)
		n, err := d.raw.Read(chunk)
		if n > 0 {
			d.appendPayload(0, chunk[:n])
		}
		if err == io.EOF && n > 0 {
			d.eof = true
			return nil
		}
		return err
	}

	header := make([]byte, 8)
	if _, err := io.ReadFull(d.raw, header); err != nil {
		return err // 干净 EOF（0 字节）或头未收满（ErrUnexpectedEOF）均向上传递
	}
	stream := header[0]
	size := binary.BigEndian.Uint32(header[4:8])
	if size > 0 {
		payload := make([]byte, size)
		if _, err := io.ReadFull(d.raw, payload); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF // 头已读但 payload 为零字节：同样是截断
			}
			return err
		}
		if stream != 1 && stream != 2 {
			stream = 1 // 未知 stream 归入 stdout，避免丢弃内容
		}
		d.appendPayload(stream, payload)
	}
	return nil
}

// appendPayload 把一帧数据并入对应 stream 的行缓冲，并切出所有完整行。
func (d *DemuxReader) appendPayload(stream byte, payload []byte) {
	d.pending[stream] = append(d.pending[stream], payload...)
	for {
		buf := d.pending[stream]
		idx := bytes.IndexByte(buf, '\n')
		if idx < 0 {
			if len(buf) > maxLogLineBytes {
				d.ready = append(d.ready, makeLogLine(stream, string(buf[:maxLogLineBytes])))
				d.pending[stream] = buf[maxLogLineBytes:]
				continue
			}
			return
		}
		d.ready = append(d.ready, makeLogLine(stream, string(buf[:idx])))
		d.pending[stream] = buf[idx+1:]
	}
}

// makeLogLine 组装一行日志，拆出 timestamps=1 产生的 RFC3339Nano 前缀。
func makeLogLine(stream byte, raw string) LogLine {
	line := LogLine{Stream: streamName(stream), Text: raw}
	if prefix, text, ok := strings.Cut(raw, " "); ok && looksLikeTimestamp(prefix) {
		line.Time = prefix
		line.Text = text
	}
	return line
}

func streamName(stream byte) string {
	if stream == 2 {
		return "stderr"
	}
	return "stdout"
}

// looksLikeTimestamp 判断前缀是否形如 RFC3339（4 位年份 + 日期分隔 + T）。
func looksLikeTimestamp(prefix string) bool {
	return len(prefix) >= 20 && prefix[4] == '-' && prefix[7] == '-' && prefix[10] == 'T'
}
