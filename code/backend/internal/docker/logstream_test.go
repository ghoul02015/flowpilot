package docker

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

// frame 构造一个 Docker multiplex 日志帧。
func frame(stream byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
	return append(header, []byte(payload)...)
}

func readAllLines(t *testing.T, reader *DemuxReader) []LogLine {
	t.Helper()
	var lines []LogLine
	for {
		line, err := reader.ReadLine()
		if err == io.EOF {
			return lines
		}
		if err != nil {
			t.Fatalf("ReadLine 返回意外错误：%v", err)
		}
		lines = append(lines, line)
	}
}

// 非 TTY 混合 stdout/stderr 多帧：行内容、stream 归属、时间戳拆分均应正确。
func TestDemuxReaderMixedFrames(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(frame(1, "2026-11-10T08:00:00.000000001Z server started\n"))
	stream.Write(frame(2, "2026-11-10T08:00:01.000000000Z warn: "))
	stream.Write(frame(2, "disk almost full\n"))
	stream.Write(frame(1, "plain line without ts\n"))

	lines := readAllLines(t, NewDemuxReader(io.NopCloser(&stream), false))
	if len(lines) != 3 {
		t.Fatalf("应切出 3 行，得到 %d：%+v", len(lines), lines)
	}
	if lines[0].Stream != "stdout" || lines[0].Time != "2026-11-10T08:00:00.000000001Z" || lines[0].Text != "server started" {
		t.Fatalf("第一行解析错误：%+v", lines[0])
	}
	if lines[1].Stream != "stderr" || lines[1].Text != "warn: disk almost full" {
		t.Fatalf("跨帧拼接的 stderr 行解析错误：%+v", lines[1])
	}
	if lines[2].Stream != "stdout" || lines[2].Time != "" || lines[2].Text != "plain line without ts" {
		t.Fatalf("无时间戳行解析错误：%+v", lines[2])
	}
}

// 无换行的超长缓冲超过上限应截断产出，后续内容继续作为新行处理。
func TestDemuxReaderTruncatesLongLine(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(frame(1, strings.Repeat("x", maxLogLineBytes+100))) // 无换行：触发截断
	stream.Write(frame(1, "\nnext\n"))

	lines := readAllLines(t, NewDemuxReader(io.NopCloser(&stream), false))
	if len(lines) != 3 {
		t.Fatalf("应切出 3 行，得到 %d", len(lines))
	}
	if len(lines[0].Text) != maxLogLineBytes {
		t.Fatalf("首行应截断到 %d 字节，得到 %d", maxLogLineBytes, len(lines[0].Text))
	}
	if len(lines[1].Text) != 100 {
		t.Fatalf("次行应为截断剩余的 100 字节，得到 %d", len(lines[1].Text))
	}
	if lines[2].Text != "next" {
		t.Fatalf("末行应为 next，得到 %q", lines[2].Text)
	}
}

// 帧头或 payload 未收满即 EOF：视为截断错误而非干净结束。
func TestDemuxReaderTruncatedFrame(t *testing.T) {
	t.Run("partial header", func(t *testing.T) {
		reader := NewDemuxReader(io.NopCloser(bytes.NewReader([]byte{1, 0, 0})), false)
		if _, err := reader.ReadLine(); err == nil || err == io.EOF {
			t.Fatalf("半帧头应返回截断错误，得到 %v", err)
		}
	})
	t.Run("partial payload", func(t *testing.T) {
		data := frame(1, "hello world\n")
		reader := NewDemuxReader(io.NopCloser(bytes.NewReader(data[:len(data)-3])), false)
		if _, err := reader.ReadLine(); err == nil || err == io.EOF {
			t.Fatalf("payload 未收满应返回截断错误，得到 %v", err)
		}
	})
}

// 干净 EOF 时缓冲中未带换行的残余数据应作为最后一行输出。
func TestDemuxReaderFlushesPendingOnEOF(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(frame(1, "line with newline\n"))
	stream.Write(frame(2, "tail without newline"))

	lines := readAllLines(t, NewDemuxReader(io.NopCloser(&stream), false))
	if len(lines) != 2 {
		t.Fatalf("应切出 2 行，得到 %d：%+v", len(lines), lines)
	}
	if lines[1].Stream != "stderr" || lines[1].Text != "tail without newline" {
		t.Fatalf("残余行解析错误：%+v", lines[1])
	}
}

// TTY 容器为裸流，直接按行切分。
func TestDemuxReaderTTYPassthrough(t *testing.T) {
	raw := "2026-11-10T08:00:00Z tty line one\ntty line two"
	lines := readAllLines(t, NewDemuxReader(io.NopCloser(strings.NewReader(raw)), true))
	if len(lines) != 2 {
		t.Fatalf("应切出 2 行，得到 %d：%+v", len(lines), lines)
	}
	if lines[0].Stream != "stdout" || lines[0].Time == "" || lines[0].Text != "tty line one" {
		t.Fatalf("TTY 行解析错误：%+v", lines[0])
	}
	if lines[1].Text != "tty line two" {
		t.Fatalf("TTY 残余行解析错误：%+v", lines[1])
	}
}
