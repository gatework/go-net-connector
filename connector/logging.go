package connector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gatework/go-net-connector/internal/terminal"
)

func logPriority(level string) int {
	switch level {
	case "debug":
		return 0
	case "info":
		return 1
	case "warn":
		return 2
	case "error":
		return 3
	default:
		return -1
	}
}

// event records bounded metadata at an operation boundary. Raw injected writers
// have no event policy; structured DEBUG output still passes the streaming
// renderer and both redaction passes before it reaches writeRecord.
func (l *logWriter) event(name, level string, c Command, status string) error {
	if l.level == "" || l.target == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if logPriority(level) < logPriority(l.level) {
		return nil
	}
	if err := l.flushLocked(); err != nil {
		return err
	}
	text := c.Text
	if c.Sensitive {
		text = "[REDACTED]"
	} else {
		text = l.safeEventText(text)
	}
	message := name
	switch name {
	case "connect":
		message = "正在连接 " + l.safeEventText(l.host)
	case "login_complete":
		message = "登录成功"
	case "command_start":
		message = "正在发送命令：" + text
	case "command_complete":
		message = "命令输出接收完成，已收到设备提示符"
	case "operation_complete":
		message = "设备脚本执行完成"
	}
	if status == "failed" {
		message = "设备操作失败"
	}
	fields := "event=" + name
	if c.Text != "" {
		fields += " text=" + strconv.Quote(text)
	}
	if status != "" {
		fields += " status=" + status
	}
	return l.writeRecord(level, message+" | "+fields)
}

func (l *logWriter) safeEventText(s string) string {
	for _, secret := range l.secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	rendered, err := terminal.Render([]byte(s), false)
	if err == nil {
		s = rendered
	}
	for _, secret := range l.secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}
func (l *logWriter) writeRecord(level, message string) error {
	line := fmt.Sprintf("[%s] %s [host=%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000 -07:00"), strings.ToUpper(level), strconv.Quote(l.safeEventText(l.host)), message)
	return l.writeTarget([]byte(line))
}
func (l *logWriter) writeTarget(p []byte) error {
	n, err := l.target.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return newError("log_error", "logging", Command{}, err)
	}
	return nil
}
func (l *logWriter) flushLine() error {
	if l.line.Len() == 0 {
		return nil
	}
	line := l.line.String()
	l.line.Reset()
	return l.writeRecord("debug", "设备输出： | event=device_output output="+strconv.Quote(line))
}

// sessionEvent keeps progress observation independent of log level and raw sinks.
func (d *Device) sessionEvent(ctx context.Context, name, level string, command Command, status string) error {
	var observed error
	if observer := d.config.OnSessionEvent; observer != nil {
		if err := observer(ctx, SessionEvent{Host: d.config.Host, Name: name, Status: status}); err != nil {
			observed = newError("session_observer_failed", "progress", Command{}, err)
		}
	}
	logged := d.log.event(name, level, command, status)
	if observed == nil {
		return logged
	}
	return errors.Join(observed, logged)
}
