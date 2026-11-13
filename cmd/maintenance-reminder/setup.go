package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"
)

const (
	portRangeStart = 20000
	portRangeEnd   = 50000
)

var commonPorts = map[int]struct{}{
	3000: {}, 3306: {}, 4000: {}, 5000: {}, 5432: {}, 5672: {}, 5900: {}, 6379: {}, 7000: {}, 8000: {}, 8080: {}, 8081: {}, 8443: {}, 8888: {}, 9000: {}, 9090: {}, 9200: {}, 11211: {}, 27017: {},
}

func recommendListener() (net.Listener, error) {
	for port := portRangeStart; port <= portRangeEnd; port++ {
		if _, common := commonPorts[port]; common {
			continue
		}
		listener, err := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
		if err == nil {
			return listener, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) && !errors.Is(err, syscall.EACCES) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no available port in %d-%d", portRangeStart, portRangeEnd)
}

func listenOnPort(text string) (net.Listener, error) {
	port, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || port < 1024 || port > 65535 {
		return nil, fmt.Errorf("端口必须是 1024 到 65535 之间的数字")
	}
	return net.Listen("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
}

type portChooser struct {
	Recommend func() (net.Listener, error)
	Listen    func(string) (net.Listener, error)
}

type lineReader interface {
	ReadString(byte) (string, error)
}

// fdLineReader reads from a terminal one byte at a time so that no input is
// buffered ahead of the hidden password prompt, which reads from the same fd.
type fdLineReader struct{ file *os.File }

func (r fdLineReader) ReadString(delim byte) (string, error) {
	var builder strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.file.Read(buf)
		if n > 0 {
			builder.WriteByte(buf[0])
			if buf[0] == delim {
				return builder.String(), nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && builder.Len() > 0 {
				return builder.String(), nil
			}
			return builder.String(), err
		}
	}
}

func chooseListener(in lineReader, out io.Writer) (net.Listener, error) {
	return (portChooser{Recommend: recommendListener, Listen: listenOnPort}).choose(in, out)
}

func (p portChooser) choose(in lineReader, out io.Writer) (net.Listener, error) {
	recommended, err := p.Recommend()
	if err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintf(out, "检测到可用推荐端口：%s\n输入端口并回车使用，直接回车接受推荐端口：", portOf(recommended.Addr()))
	for {
		line, readErr := in.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			_ = recommended.Close()
			return nil, readErr
		}
		selected := strings.TrimSpace(line)
		if selected == "" || selected == portOf(recommended.Addr()) {
			return recommended, nil
		}
		listener, bindErr := p.Listen(selected)
		if bindErr == nil {
			_ = recommended.Close()
			return listener, nil
		}
		_, _ = fmt.Fprintf(out, "端口不可用（%v），请重新输入：", bindErr)
		if readErr != nil {
			_ = recommended.Close()
			return nil, fmt.Errorf("no valid port selected")
		}
	}
}

func portOf(addr net.Addr) string { _, port, _ := net.SplitHostPort(addr.String()); return port }

// promptPassword reads a hidden password twice and verifies it matches and
// satisfies the minimum length. The plaintext is never printed or persisted.
func promptPassword(in *os.File, out io.Writer) (string, error) {
	return promptPasswordWith(readHiddenFunc(in, out), out)
}

type hiddenFunc func() (string, error)

func promptPasswordWith(read hiddenFunc, out io.Writer) (string, error) {
	for attempt := 1; attempt <= 3; attempt++ {
		fmt.Fprint(out, "管理员密码（至少 12 位，不会回显）：")
		password, err := read()
		if err != nil {
			return "", err
		}
		if len(password) < 12 {
			_, _ = fmt.Fprintf(out, "密码至少需要 12 位（还剩 %d 次尝试）\n", 3-attempt)
			continue
		}
		fmt.Fprint(out, "再次输入管理员密码：")
		confirm, err := read()
		if err != nil {
			return "", err
		}
		if password != confirm {
			_, _ = fmt.Fprintf(out, "两次密码不一致（还剩 %d 次尝试）\n", 3-attempt)
			continue
		}
		return password, nil
	}
	return "", fmt.Errorf("密码校验失败，请重新运行初始化")
}

func readHiddenFunc(in *os.File, out io.Writer) hiddenFunc {
	return func() (string, error) {
		if in == nil {
			return "", fmt.Errorf("缺少输入终端")
		}
		if !term.IsTerminal(int(in.Fd())) {
			return "", fmt.Errorf("密码输入需要交互式终端，请在服务器控制台运行程序")
		}
		bytes, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(out)
		if err != nil {
			return "", err
		}
		return string(bytes), nil
	}
}
